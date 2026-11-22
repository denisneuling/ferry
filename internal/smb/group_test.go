package smb

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// observed records what a group tells its observer.
type observed struct {
	mu    sync.Mutex
	open  int
	lost  map[string]int
	waits map[string][]time.Duration
}

func newObserved() *observed {
	return &observed{lost: map[string]int{}, waits: map[string][]time.Duration{}}
}

func (o *observed) Connections(d int) { o.mu.Lock(); o.open += d; o.mu.Unlock() }
func (o *observed) ConnectionLost(pool string) {
	o.mu.Lock()
	o.lost[pool]++
	o.mu.Unlock()
}
func (o *observed) SlotWait(pool string, d time.Duration) {
	o.mu.Lock()
	o.waits[pool] = append(o.waits[pool], d)
	o.mu.Unlock()
}

func (o *observed) snapshot() (open int, lost map[string]int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	l := map[string]int{}
	for k, v := range o.lost {
		l[k] = v
	}
	return o.open, l
}

func loginGroup(t testing.TB, server string, n int, obs Observer, user, password string) (*Group, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	opts := LoginOptions{Server: server, Connections: n}
	if obs != nil {
		opts.Observer = obs
	}
	return LoginGroup(ctx, opts, Credentials{User: user, Password: password})
}

// A group of three is three authenticated sessions on three connections:
// background work is spread over two of them and never handed the
// interactive one while they last; closing closes all three.
func TestGroup(t *testing.T) {
	obs := newObserved()
	g, err := loginGroup(t, sambaServer(t), 3, obs, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	if g.Size() != 3 {
		t.Fatalf("size %d", g.Size())
	}
	if open, _ := obs.snapshot(); open != 3 {
		t.Fatalf("observer counts %d open", open)
	}
	b1, done1 := g.Background()
	b2, done2 := g.Background()
	if b1 == g.Interactive() || b2 == g.Interactive() || b1 == b2 {
		t.Fatal("background work not spread over the background connections")
	}
	done1()
	done2()
	ctx := context.Background()
	for _, s := range []*Session{g.Interactive(), b1, b2} {
		if _, err := s.ReadDir(ctx, "alex", "", 10); err != nil {
			t.Fatalf("a connection of the group does not work: %v", err)
		}
	}
	if s := b1.s; s == g.Interactive().s || b1.conn == g.Interactive().conn {
		t.Fatal("connections shared")
	}
	_ = g.Close()
	if open, lost := obs.snapshot(); open != 0 || len(lost) != 0 {
		t.Fatalf("after close: %d open, lost %v", open, lost)
	}
	if !g.Lost() {
		t.Fatal("a closed group is not lost")
	}
}

// A group is all or nothing: a refused password or a guest offer refuses the
// whole login, and nothing stays open.
func TestGroupRefusedWhole(t *testing.T) {
	cases := []struct {
		name, server, user, password string
		want                         Kind
	}{
		{"wrong password", sambaServer(t), "alex", "not-the-password", KindAuth},
		{"unknown user, mapped to guest", sambaServer(t), "nosuchuser", "whatever", KindGuest},
	}
	if bad := os.Getenv("FERRY_TEST_SMB_SERVER_BAD_PASSWORD"); bad != "" {
		cases = append(cases, struct {
			name, server, user, password string
			want                         Kind
		}{"wrong password, mapped to guest", bad, "alex", "not-the-password", KindGuest})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obs := newObserved()
			g, err := loginGroup(t, c.server, 3, obs, c.user, c.password)
			if err == nil {
				_ = g.Close()
				t.Fatal("got a group")
			}
			if KindOf(err) != c.want {
				t.Fatalf("got %v, want %s", err, c.want)
			}
			if open, _ := obs.snapshot(); open != 0 {
				t.Fatalf("%d connections counted open", open)
			}
		})
	}
}

// A wrong password costs the server one session setup per login, not one
// per connection: its audit log and any lockout count see one attempt. A
// right one sets up all of them.
func TestGroupRefusedAfterOneSetup(t *testing.T) {
	server := sambaServer(t)
	var mu sync.Mutex
	setups := 0
	setup = func(ctx context.Context, opts LoginOptions, cred Credentials) (*Session, error) {
		mu.Lock()
		setups++
		mu.Unlock()
		return Login(ctx, opts, cred)
	}
	t.Cleanup(func() { setup = Login })

	if _, err := loginGroup(t, server, 3, nil, "alex", "not-the-password"); KindOf(err) != KindAuth {
		t.Fatalf("wrong password: %v", err)
	}
	if setups != 1 {
		t.Fatalf("a wrong password cost %d session setups", setups)
	}
	setups = 0
	g, err := loginGroup(t, server, 3, nil, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	_ = g.Close()
	if setups != 3 {
		t.Fatalf("a right password made %d session setups", setups)
	}
}

// A background connection that goes away leaves the group smaller and the
// person logged in; with none left, background work shares the interactive
// connection. The interactive one gone, the group is lost — counted once.
func TestGroupLosesConnections(t *testing.T) {
	obs := newObserved()
	g, err := loginGroup(t, sambaServer(t), 3, obs, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	ctx := context.Background()

	b, done := g.Background()
	done()
	_ = b.conn.Close()
	if _, err := b.ReadDir(ctx, "alex", "", 10); KindOf(err) != KindSessionLost {
		t.Fatalf("on the dropped connection: %v", err)
	}
	if g.Lost() || g.Size() != 2 {
		t.Fatalf("lost %v, size %d", g.Lost(), g.Size())
	}
	if open, lost := obs.snapshot(); open != 2 || lost[PoolBackground] != 1 {
		t.Fatalf("observer: %d open, lost %v", open, lost)
	}
	next, done := g.Background()
	done()
	if next == b || next == g.Interactive() {
		t.Fatal("the lost connection, or the interactive one, was handed out")
	}

	_ = next.conn.Close()
	_, _ = next.ReadDir(ctx, "alex", "", 10)
	last, done := g.Background()
	done()
	if last != g.Interactive() || g.Size() != 1 {
		t.Fatalf("with no background left: size %d, interactive handed out %v", g.Size(), last == g.Interactive())
	}
	if _, err := last.ReadDir(ctx, "alex", "", 10); err != nil {
		t.Fatalf("the interactive connection carries the background: %v", err)
	}

	_ = g.Interactive().conn.Close()
	_, _ = g.Interactive().ReadDir(ctx, "alex", "", 10)
	for range 3 {
		if !g.Lost() {
			t.Fatal("the group outlives its interactive connection")
		}
	}
	_ = g.Close()
	if open, lost := obs.snapshot(); open != 0 || lost[PoolInteractive] != 1 || lost[PoolBackground] != 2 {
		t.Fatalf("after close: %d open, lost %v", open, lost)
	}
}

// What a group is for: while downloads keep every background connection
// busy, a listing on the interactive one does not wait for its slot.
func TestGroupInteractiveNotHeldUp(t *testing.T) {
	obs := newObserved()
	g, err := loginGroup(t, sambaServer(t), 3, obs, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for range 4 {
		s, done := g.Background()
		wg.Go(func() {
			defer done()
			for ctx.Err() == nil {
				f, err := s.Open(ctx, "alex", "big.bin")
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, f)
				_ = f.Close()
			}
		})
	}
	time.Sleep(200 * time.Millisecond)
	for range 20 {
		if _, err := g.Interactive().ReadDir(ctx, "alex", "", 50); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	wg.Wait()
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.waits[PoolBackground]) == 0 {
		t.Fatal("the downloads did not run")
	}
	for _, w := range obs.waits[PoolInteractive] {
		if w > 20*time.Millisecond {
			t.Errorf("a listing waited %v for the interactive slot", w)
		}
	}
}
