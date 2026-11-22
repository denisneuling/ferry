package smb

import (
	"context"
	"sync"
	"time"
)

// The pools of a group, as the metrics name them.
const (
	PoolInteractive = "interactive"
	PoolBackground  = "background"
)

// MaxConnections bounds LoginOptions.Connections.
const MaxConnections = 4

// Observer hears of a group's connections, for the metrics. Nothing it is
// told names a person, a share or a file.
type Observer interface {
	// Connections is told of connections opened (positive) and closed or
	// lost (negative).
	Connections(delta int)
	// ConnectionLost is told of a connection that went away on its own.
	ConnectionLost(pool string)
	// SlotWait is told how long a request waited for a connection's slot.
	SlotWait(pool string, d time.Duration)
}

// A Group is one person's SMB sessions, each on its own connection, all
// set up at login with the same credentials — the only moment the password
// exists. One serves interactive work: listing, stat, creating, renaming,
// deleting; the others serve the background: thumbnails, details, content.
// A folder opened therefore never waits behind thumbnails loading, while
// each connection still carries one request at a time (Session.op).
//
// A group never reconnects. Its interactive connection lost, the group is
// lost and the person logs in again; a background connection lost, the
// group carries on with the ones left, and with none left the background
// work shares the interactive connection, as with a group of one.
//
// The connections are never shared: a group is one identity's.
type Group struct {
	interactive *Session
	obs         Observer

	mu         sync.Mutex
	background []*Session
	busy       map[*Session]int // background requests assigned now
	closed     bool
}

// LoginGroup sets up opts.Connections sessions with exactly these
// credentials, each as Login does — the guest guard included. The first,
// the interactive one, alone: a wrong password then costs the server one
// failed session setup, not one per connection, so that its audit log and
// any lockout counting (a domain's bad-password count, pam_faillock,
// fail2ban) see one attempt per login. Only when it succeeded are the
// others set up, at once. It succeeds only if every one does: one refused,
// or a guest, and all are closed again and the error of the first refused
// one is returned.
func LoginGroup(ctx context.Context, opts LoginOptions, cred Credentials) (*Group, error) {
	n := min(max(opts.Connections, 1), MaxConnections)
	first, err := setup(ctx, opts, cred)
	if err != nil {
		return nil, err
	}
	sessions := make([]*Session, n)
	errs := make([]error, n)
	sessions[0] = first
	var wg sync.WaitGroup
	for i := 1; i < n; i++ {
		wg.Go(func() { sessions[i], errs[i] = setup(ctx, opts, cred) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			for _, s := range sessions {
				if s != nil {
					_ = s.Close()
				}
			}
			return nil, err
		}
	}
	g := &Group{interactive: first, obs: opts.Observer, background: sessions[1:], busy: map[*Session]int{}}
	for _, s := range g.background {
		s.pool = PoolBackground
	}
	if g.obs != nil {
		g.obs.Connections(n)
	}
	return g, nil
}

// setup is Login; a test counts the session setups through it.
var setup = Login

// Interactive is the session for what a person waits on to go on: a
// listing, a stat, a change.
func (g *Group) Interactive() *Session { return g.interactive }

// Background is a session for what loads alongside: the one of the
// background connections with the fewest requests assigned, or the
// interactive one when there are none (left). The caller calls done when
// its request is over. A request holds a connection's slot only per step
// (call.go), so this spreads the load and promises no slot.
func (g *Group) Background() (s *Session, done func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dropLostLocked()
	for _, b := range g.background {
		if s == nil || g.busy[b] < g.busy[s] {
			s = b
		}
	}
	if s == nil {
		return g.interactive, func() {}
	}
	g.busy[s]++
	return s, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.busy[s]--; g.busy[s] <= 0 {
			delete(g.busy, s)
		}
	}
}

// dropLostLocked takes the background connections that went away out of
// the group, and tells the observer.
func (g *Group) dropLostLocked() {
	if g.closed {
		return
	}
	live := g.background[:0]
	for _, b := range g.background {
		if !b.Lost() {
			live = append(live, b)
			continue
		}
		_ = b.Close()
		if g.obs != nil {
			g.obs.ConnectionLost(PoolBackground)
			g.obs.Connections(-1)
		}
	}
	clear(g.background[len(live):])
	g.background = live
}

// Size is how many connections the group has now.
func (g *Group) Size() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dropLostLocked()
	return 1 + len(g.background)
}

// Lost reports whether the group can no longer be used: its interactive
// connection is gone. session.Table ends it then.
func (g *Group) Lost() bool { return g.interactive.Lost() }

// Close logs off every connection, at once, and is idempotent. A group
// whose interactive connection went away on its own is counted lost here,
// once, however often Lost was asked before.
func (g *Group) Close() error {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil
	}
	g.dropLostLocked() // background connections lost before are counted as such
	g.closed = true
	all := append([]*Session{g.interactive}, g.background...)
	g.background = nil
	g.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range all {
		wg.Go(func() { _ = s.Close() })
	}
	wg.Wait()
	if g.obs != nil {
		if g.interactive.wentAway() {
			g.obs.ConnectionLost(PoolInteractive)
		}
		g.obs.Connections(-len(all))
	}
	return nil
}
