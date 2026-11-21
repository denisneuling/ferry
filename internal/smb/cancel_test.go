package smb

import (
	"context"
	"io"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// Requests a browser abandons — a thumbnail scrolled away, a folder left
// before its listing came — end their context in the middle of an SMB call.
// That must cost the answer, never the session: afterwards the same session
// lists, opens and reads as before, and is not marked lost.
func TestCancelledCallsKeepTheSession(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	r := rand.New(rand.NewSource(1))
	var wg sync.WaitGroup
	for i := 0; i < 400; i++ {
		d := time.Duration(r.Intn(3000)) * time.Microsecond
		op := r.Intn(4)
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), d)
			defer cancel()
			switch op {
			case 0:
				_, _ = s.ReadDir(ctx, "alex", "", 100)
			case 1:
				_, _ = s.Stat(ctx, "alex", "hello.txt")
			case 2:
				if f, err := s.Open(ctx, "alex", "big.bin"); err == nil {
					_, _ = io.CopyN(io.Discard, f, 4<<20)
					_ = f.Close()
				}
			case 3:
				_, _ = s.Shares(ctx)
			}
		}()
		if i%20 == 0 {
			wg.Wait()
		}
	}
	wg.Wait()
	if s.Lost() {
		t.Fatal("the session was marked lost")
	}
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		if _, err := s.ReadDir(ctx, "alex", "", 100); err != nil {
			t.Fatalf("listing after the cancellations: %v", err)
		}
		f, err := s.Open(ctx, "alex", "big.bin")
		if err != nil {
			t.Fatalf("open after the cancellations: %v", err)
		}
		if n, err := io.CopyN(io.Discard, f, 8<<20); err != nil || n != 8<<20 {
			t.Fatalf("read after the cancellations: %d %v", n, err)
		}
		_ = f.Close()
	}
}
