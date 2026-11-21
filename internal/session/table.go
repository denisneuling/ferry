// Package session keeps ferry's sessions: a table in memory from a random
// session ID to one live SMB session, with an idle timeout, an absolute
// lifetime and a bound on how many there are. Nothing in it is ever written
// anywhere; a restart ends every session.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Conn is what the table needs of an SMB session: whether it still works,
// and how to end it. *smb.Session satisfies it.
type Conn interface {
	Lost() bool
	Close() error
}

// ErrFull is returned by Create when the table holds MaxSessions.
var ErrFull = errors.New("session: the table is full")

// Options bound the table.
type Options struct {
	IdleTimeout time.Duration
	Lifetime    time.Duration
	MaxSessions int
	// Now is the clock; time.Now when nil. Tests set it.
	Now func() time.Time
}

// Session is one logged-in person.
type Session struct {
	Conn    Conn
	User    string
	Created time.Time
	Expires time.Time // Created + Lifetime: the end, whatever the activity

	// Guarded by the table's mutex.
	lastUsed time.Time
	active   int
}

// Table is the session table. It is safe for concurrent use.
type Table struct {
	opts Options

	mu       sync.Mutex
	sessions map[[32]byte]*Session
}

// New makes an empty table.
func New(opts Options) *Table {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Table{opts: opts, sessions: map[[32]byte]*Session{}}
}

// key is what the table stores a session under: the SHA-256 of its ID, so
// that the table holds nothing a cookie could be made from.
func key(id string) [32]byte { return sha256.Sum256([]byte(id)) }

// Create adds a session for a fresh SMB session and returns its ID: 256
// random bits, base64url. With the table full it first sweeps, then
// refuses with ErrFull; the caller closes conn then.
func (t *Table) Create(conn Conn, user string) (string, *Session, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	now := t.opts.Now()
	s := &Session{Conn: conn, User: user, Created: now, Expires: now.Add(t.opts.Lifetime), lastUsed: now}

	t.mu.Lock()
	if len(t.sessions) >= t.opts.MaxSessions {
		ended := t.collectLocked(now)
		t.mu.Unlock()
		closeAll(ended)
		t.mu.Lock()
	}
	defer t.mu.Unlock()
	if len(t.sessions) >= t.opts.MaxSessions {
		return "", nil, ErrFull
	}
	t.sessions[key(id)] = s
	return id, s, nil
}

// Acquire returns the session for an ID if it is still alive, and marks it in
// use: the idle timeout does not end a session while a request uses it, so a
// download longer than the idle timeout is not cut off. Every Acquire must
// be followed by Release.
func (t *Table) Acquire(id string) (*Session, bool) {
	if id == "" {
		return nil, false
	}
	now := t.opts.Now()
	t.mu.Lock()
	k := key(id)
	s, ok := t.sessions[k]
	if ok && t.expiredLocked(s, now) {
		delete(t.sessions, k)
		t.mu.Unlock()
		_ = s.Conn.Close()
		return nil, false
	}
	if !ok {
		t.mu.Unlock()
		return nil, false
	}
	s.active++
	s.lastUsed = now
	t.mu.Unlock()
	return s, true
}

// Release ends a use begun with Acquire; the session's idle time counts from
// here.
func (t *Table) Release(s *Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s.active--
	s.lastUsed = t.opts.Now()
}

// Delete ends the session with this ID, if there is one, and closes its SMB
// session.
func (t *Table) Delete(id string) {
	t.mu.Lock()
	k := key(id)
	s, ok := t.sessions[k]
	if ok {
		delete(t.sessions, k)
	}
	t.mu.Unlock()
	if ok {
		_ = s.Conn.Close()
	}
}

// Len is the number of sessions in the table.
func (t *Table) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

// Sweep ends every session past its idle timeout or lifetime, or whose SMB
// connection is gone, and returns how many it ended.
func (t *Table) Sweep() int {
	t.mu.Lock()
	ended := t.collectLocked(t.opts.Now())
	t.mu.Unlock()
	closeAll(ended)
	return len(ended)
}

// Run sweeps every interval until ctx ends, then ends every session.
func (t *Table) Run(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			t.CloseAll()
			return
		case <-tick.C:
			t.Sweep()
		}
	}
}

// CloseAll ends every session, as a shutdown does.
func (t *Table) CloseAll() {
	t.mu.Lock()
	ended := make([]*Session, 0, len(t.sessions))
	for k, s := range t.sessions {
		delete(t.sessions, k)
		ended = append(ended, s)
	}
	t.mu.Unlock()
	closeAll(ended)
}

func (t *Table) expiredLocked(s *Session, now time.Time) bool {
	if !now.Before(s.Expires) || s.Conn.Lost() {
		return true
	}
	return s.active == 0 && now.Sub(s.lastUsed) >= t.opts.IdleTimeout
}

func (t *Table) collectLocked(now time.Time) []*Session {
	var ended []*Session
	for k, s := range t.sessions {
		if t.expiredLocked(s, now) {
			delete(t.sessions, k)
			ended = append(ended, s)
		}
	}
	return ended
}

// closeAll closes SMB sessions outside the table's lock: logging off may
// wait for the server.
func closeAll(ss []*Session) {
	var wg sync.WaitGroup
	for _, s := range ss {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Conn.Close()
		}()
	}
	wg.Wait()
}
