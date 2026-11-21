package session

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeConn struct {
	lost   atomic.Bool
	closed atomic.Int32
}

func (c *fakeConn) Lost() bool   { return c.lost.Load() }
func (c *fakeConn) Close() error { c.closed.Add(1); return nil }

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func newTable(c *clock, max int) *Table {
	return New(Options{IdleTimeout: 30 * time.Minute, Lifetime: 12 * time.Hour, MaxSessions: max, Now: c.now})
}

func TestCreateAcquireDelete(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	tab := newTable(c, 10)
	conn := &fakeConn{}
	id, s, err := tab.Create(conn, "alex")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 43 || s.User != "alex" || !s.Expires.Equal(c.now().Add(12*time.Hour)) {
		t.Fatalf("id %q session %+v", id, s)
	}
	got, ok := tab.Acquire(id)
	if !ok || got != s {
		t.Fatal("not found")
	}
	tab.Release(got)
	if _, ok := tab.Acquire(id + "x"); ok {
		t.Fatal("found by a wrong id")
	}
	if _, ok := tab.Acquire(""); ok {
		t.Fatal("found by an empty id")
	}
	tab.Delete(id)
	if _, ok := tab.Acquire(id); ok || conn.closed.Load() != 1 {
		t.Fatalf("after delete: found=%v closed=%d", ok, conn.closed.Load())
	}
	tab.Delete(id) // idempotent
	if conn.closed.Load() != 1 {
		t.Fatal("closed twice")
	}
}

func TestIDsAreUnique(t *testing.T) {
	tab := newTable(&clock{t: time.Now()}, 1000)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id, _, err := tab.Create(&fakeConn{}, "alex")
		if err != nil || seen[id] {
			t.Fatalf("id %d: %q %v", i, id, err)
		}
		seen[id] = true
	}
}

func TestIdleAndLifetime(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	tab := newTable(c, 10)

	idle := &fakeConn{}
	idleID, _, _ := tab.Create(idle, "alex")
	c.add(29 * time.Minute)
	if s, ok := tab.Acquire(idleID); !ok {
		t.Fatal("ended before the idle timeout")
	} else {
		tab.Release(s)
	}
	c.add(30 * time.Minute)
	if _, ok := tab.Acquire(idleID); ok || idle.closed.Load() != 1 {
		t.Fatal("not ended after the idle timeout")
	}

	// A session in use is not idled out — a long download — but its
	// lifetime ends it regardless.
	busy := &fakeConn{}
	busyID, _, _ := tab.Create(busy, "sam")
	s, _ := tab.Acquire(busyID)
	c.add(2 * time.Hour)
	if tab.Sweep() != 0 || busy.closed.Load() != 0 {
		t.Fatal("a session in use was idled out")
	}
	tab.Release(s)
	c.add(10 * time.Minute)
	if s, ok := tab.Acquire(busyID); !ok {
		t.Fatal("idle time did not restart at release")
	} else {
		tab.Release(s)
	}
	for i := 0; i < 50; i++ {
		c.add(20 * time.Minute)
		if s, ok := tab.Acquire(busyID); ok {
			tab.Release(s)
		}
	}
	if _, ok := tab.Acquire(busyID); ok || busy.closed.Load() != 1 {
		t.Fatal("not ended after its lifetime")
	}
}

func TestLostConnectionEndsTheSession(t *testing.T) {
	tab := newTable(&clock{t: time.Now()}, 10)
	conn := &fakeConn{}
	id, _, _ := tab.Create(conn, "alex")
	conn.lost.Store(true)
	if _, ok := tab.Acquire(id); ok {
		t.Fatal("a lost session was handed out")
	}
	if tab.Len() != 0 || conn.closed.Load() != 1 {
		t.Fatalf("len %d closed %d", tab.Len(), conn.closed.Load())
	}
}

func TestFull(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	tab := newTable(c, 2)
	_, _, _ = tab.Create(&fakeConn{}, "a")
	_, _, _ = tab.Create(&fakeConn{}, "b")
	if _, _, err := tab.Create(&fakeConn{}, "c"); err != ErrFull {
		t.Fatalf("got %v, want ErrFull", err)
	}
	// Expired sessions make room.
	c.add(time.Hour)
	if _, _, err := tab.Create(&fakeConn{}, "c"); err != nil {
		t.Fatalf("after the others expired: %v", err)
	}
}

func TestCloseAll(t *testing.T) {
	tab := newTable(&clock{t: time.Now()}, 10)
	var conns []*fakeConn
	for i := 0; i < 5; i++ {
		c := &fakeConn{}
		conns = append(conns, c)
		_, _, _ = tab.Create(c, fmt.Sprint(i))
	}
	tab.CloseAll()
	for i, c := range conns {
		if c.closed.Load() != 1 {
			t.Errorf("session %d not closed", i)
		}
	}
	if tab.Len() != 0 {
		t.Fatal("table not empty")
	}
}

func TestConcurrentUse(t *testing.T) {
	tab := newTable(&clock{t: time.Now()}, 1000)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, _, err := tab.Create(&fakeConn{}, "u")
			if err != nil {
				t.Error(err)
				return
			}
			for j := 0; j < 100; j++ {
				if s, ok := tab.Acquire(id); ok {
					tab.Release(s)
				}
			}
			tab.Delete(id)
		}()
	}
	wg.Wait()
	if tab.Len() != 0 {
		t.Fatalf("len %d", tab.Len())
	}
}

func TestLimiter(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	l := NewLimiter(5, 100, c.now)
	for i := 0; i < 5; i++ {
		if ok, _ := l.Allow("u:alex"); !ok {
			t.Fatalf("attempt %d refused", i)
		}
	}
	ok, wait := l.Allow("u:alex")
	if ok || wait <= 0 || wait > 12*time.Second {
		t.Fatalf("sixth attempt: %v, wait %v", ok, wait)
	}
	if ok, _ := l.Allow("u:sam"); !ok {
		t.Fatal("another key was limited")
	}
	c.add(12 * time.Second)
	if ok, _ := l.Allow("u:alex"); !ok {
		t.Fatal("not refilled")
	}
}

func TestLimiterBoundsItsKeys(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	l := NewLimiter(5, 10, c.now)
	for i := 0; i < 1000; i++ {
		l.Allow(fmt.Sprint("a:", i))
		c.add(time.Millisecond)
	}
	if n := len(l.buckets); n > 10 {
		t.Fatalf("%d buckets", n)
	}
}
