package smb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// manyEntries is the size of hack/samba's alex/Many.
const manyEntries = 60000

// TestDirLargeFolder lists a folder far larger than a batch, batch by batch,
// and finds every name exactly once.
func TestDirLargeFolder(t *testing.T) {
	s, err := login(t, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	start := time.Now()
	d, err := s.OpenDir(ctx, "alex", "Many", 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var first time.Duration
	for {
		batch, err := d.Next()
		if first == 0 && len(batch) > 0 {
			first = time.Since(start)
		}
		if len(batch) > DirBatch {
			t.Fatalf("a batch of %d, more than %d", len(batch), DirBatch)
		}
		for _, e := range batch {
			if seen[e.Name] {
				t.Fatalf("%s twice", e.Name)
			}
			seen[e.Name] = true
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != manyEntries {
		t.Fatalf("%d entries, want %d", len(seen), manyEntries)
	}
	t.Logf("%d entries: first batch after %v, all after %v", len(seen), first, time.Since(start))
	if _, err := d.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("after the end: %v", err)
	}
}

// TestDirLimit refuses a folder beyond the bound with KindLimit carrying the
// bound, and the session goes on.
func TestDirLimit(t *testing.T) {
	s, err := login(t, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	_, err = s.ReadDir(ctx, "alex", "Many", 1000)
	if KindOf(err) != KindLimit || LimitOf(err) != 1000 {
		t.Fatalf("got %v (limit %d), want a limit of 1000", err, LimitOf(err))
	}
	if _, err := s.ReadDir(ctx, "alex", "", 100); err != nil {
		t.Fatalf("the session after the limit: %v", err)
	}
	if _, err := s.OpenDir(ctx, "alex", "hello.txt", 100); KindOf(err) != KindNotDir {
		t.Fatalf("a file as a folder: %v", err)
	}
}

// TestDirLeavesTheSlot shows that an open listing holds the session's
// request slot only during a batch: other calls go in between, as they
// would while a slow client reads a listing.
func TestDirLeavesTheSlot(t *testing.T) {
	s, err := login(t, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	d, err := s.OpenDir(ctx, "alex", "Many", 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Next(); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.Stat(sctx, "alex", "hello.txt"); err != nil {
		t.Fatalf("a call while a listing is open: %v", err)
	}
	if _, err := d.Next(); err != nil {
		t.Fatalf("the listing after the call: %v", err)
	}
}

// TestDirClientLeaves ends the opening request's context between batches:
// the next batch stops before its call, and the session stays usable.
func TestDirClientLeaves(t *testing.T) {
	s, err := login(t, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	d, err := s.OpenDir(ctx, "alex", "Many", 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Next(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := d.Next(); KindOf(err) != KindCanceled {
		t.Fatalf("after the client left: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("closing after the client left: %v", err)
	}
	if _, err := s.ReadDir(context.Background(), "alex", "", 100); err != nil {
		t.Fatalf("the session afterwards: %v", err)
	}
}

// TestDirWhileTheFolderChanges lists a folder while another session adds,
// renames and removes files in it, as a scanner or a second person would:
// the listing ends normally, and no name comes twice. What changed during
// it may or may not be in it; that is the server's to decide.
func TestDirWhileTheFolderChanges(t *testing.T) {
	ctx := context.Background()
	s, err := login(t, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	other, err := login(t, "alex", alexPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()

	dir := fmt.Sprintf("changing-%d", time.Now().UnixNano())
	if _, err := s.Mkdir(ctx, "alex", dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Remove(ctx, "alex", dir, true, 100000) }()
	for i := 0; i < 3*DirBatch; i++ {
		if _, err := s.Upload(ctx, "alex", fmt.Sprintf("%s/before-%04d.txt", dir, i), strings.NewReader("x"), false, 0); err != nil {
			t.Fatal(err)
		}
	}

	d, err := s.OpenDir(ctx, "alex", dir, 100000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	seen := map[string]int{}
	for round := 0; ; round++ {
		batch, err := d.Next()
		for _, e := range batch {
			seen[e.Name]++
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("the listing broke off while the folder changed: %v", err)
		}
		// Between batches, the folder changes under the listing.
		for i := 0; i < 50; i++ {
			name := fmt.Sprintf("%s/during-%d-%02d.txt", dir, round, i)
			if _, err := other.Upload(ctx, "alex", name, strings.NewReader("y"), false, 0); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = other.Rename(ctx, "alex", fmt.Sprintf("%s/before-%04d.txt", dir, round), fmt.Sprintf("%s/renamed-%04d.txt", dir, round), false)
		_ = other.Remove(ctx, "alex", fmt.Sprintf("%s/before-%04d.txt", dir, 3*DirBatch-1-round), false, 0)
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("%s came %d times", name, n)
		}
	}
	t.Logf("%d names listed while the folder changed", len(seen))
}
