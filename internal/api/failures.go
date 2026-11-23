package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/denisneuling/ferry/internal/cache"
	"github.com/denisneuling/ferry/internal/preview"
	"github.com/denisneuling/ferry/internal/thumb"
)

const (
	// failureTTL is how long a file that made no thumbnail is answered
	// from memory. Short: the file's version is in the key, so a changed
	// file is tried at once anyway; this only spares a wall of the same
	// broken files being decoded on every visit.
	failureTTL = 10 * time.Minute
	// maxFailures bounds the memory: an entry is a hash, a kind and a time.
	maxFailures = 10000
)

// failures remembers, for a while, which files made no thumbnail and why:
// only what the file decides (unreadable, blank, too large), never what
// the moment does (the share, the budget, a timeout, a client that left).
// It is consulted where the cache is, once the request's own session has
// opened the file for reading, so Samba still decides who learns that a
// file makes no thumbnail. It is kept in memory: a restart, or other
// limits, start afresh. The keys are keyed hashes of the file's identity
// and version and the size asked for; no file name is kept.
type failures struct {
	mu    sync.Mutex
	key   [32]byte
	now   func() time.Time
	ttl   time.Duration
	max   int
	m     map[[32]byte]failure
	queue []queued // in the order they were remembered, and so expire
}

type failure struct {
	tooLarge bool // otherwise unsupported
	expires  time.Time
}

type queued struct {
	id      [32]byte
	expires time.Time
}

func newFailures() *failures {
	f := &failures{now: time.Now, ttl: failureTTL, max: maxFailures, m: map[[32]byte]failure{}}
	_, _ = rand.Read(f.key[:])
	return f
}

func (f *failures) id(k cache.Key) [32]byte {
	mac := hmac.New(sha256.New, f.key[:])
	for _, s := range []string{k.Kind, k.Variant, k.Share, k.Path} {
		// Lengths first, so that no two keys write the same bytes.
		_ = binary.Write(mac, binary.BigEndian, uint32(len(s)))
		mac.Write([]byte(s))
	}
	_ = binary.Write(mac, binary.BigEndian, k.Size)
	_ = binary.Write(mac, binary.BigEndian, k.ModTime.UnixNano())
	var id [32]byte
	copy(id[:], mac.Sum(nil))
	return id
}

// get returns what the file made last time, if it is remembered.
func (f *failures) get(k cache.Key) (err error, ok bool) {
	id := f.id(k)
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.m[id]
	if !ok {
		return nil, false
	}
	if !f.now().Before(e.expires) {
		delete(f.m, id)
		return nil, false
	}
	if e.tooLarge {
		return thumb.ErrTooLarge, true
	}
	return thumb.ErrUnsupported, true
}

// put remembers err for the file, when it is one the file decides.
func (f *failures) put(k cache.Key, err error) {
	if !lasting(err) {
		return
	}
	id := f.id(k)
	now := f.now()
	f.mu.Lock()
	defer f.mu.Unlock()
	// What has expired goes first, then the oldest, down to the bound.
	for len(f.queue) > 0 && (!now.Before(f.queue[0].expires) || len(f.m) >= f.max) {
		q := f.queue[0]
		f.queue = f.queue[1:]
		if e, ok := f.m[q.id]; ok && e.expires.Equal(q.expires) {
			delete(f.m, q.id)
		}
	}
	e := failure{tooLarge: isTooLarge(err), expires: now.Add(f.ttl)}
	f.m[id] = e
	f.queue = append(f.queue, queued{id, e.expires})
}

func (f *failures) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.m)
}

// lasting tells an answer the file itself decides from one of the moment.
// A preview that ran out of time, a budget that was taken, a share that
// failed or a request that ended may all go otherwise next time.
func lasting(err error) bool {
	switch {
	case err == nil,
		errors.Is(err, preview.ErrTimeout),
		errors.Is(err, preview.ErrUnavailable),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return false
	}
	return isTooLarge(err) ||
		errors.Is(err, thumb.ErrUnsupported) ||
		errors.Is(err, preview.ErrUnsupported) ||
		errors.Is(err, preview.ErrBlank)
}

func isTooLarge(err error) bool {
	return errors.Is(err, thumb.ErrTooLarge) || errors.Is(err, preview.ErrTooLarge)
}
