package smb

import (
	"context"
	"testing"
	"time"
)

// The tree hack/samba writes under alex's Search folder, added up.
func TestUsage(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	steps := 0
	u, err := s.Usage(ctx, "alex", "Search", time.Time{}, DefaultUsageLimits, func(Usage) { steps++ })
	// Five files of 9 bytes in all; a, a/b, a/b/c and leer counted; Privat,
	// which alex may see but not open, skipped.
	if err != nil || u.Files != 5 || u.Bytes != 9 || u.Folders != 4 || u.Skipped != 1 || u.Truncated {
		t.Fatalf("usage %+v, %v", u, err)
	}
	if steps != 5 {
		t.Errorf("%d progress steps, want one per folder listed", steps)
	}
	lim := DefaultUsageLimits
	lim.Depth = 1
	if u, err := s.Usage(ctx, "alex", "Search", time.Time{}, lim, func(Usage) {}); err != nil || !u.Truncated || u.Reason != "depth" || u.Files != 2 {
		t.Errorf("depth 1: %+v, %v", u, err)
	}
	lim = DefaultUsageLimits
	lim.Entries = 3
	if u, err := s.Usage(ctx, "alex", "Search", time.Time{}, lim, func(Usage) {}); err != nil || !u.Truncated || u.Reason != "entries" {
		t.Errorf("3 entries: %+v, %v", u, err)
	}
	if _, err := s.Usage(ctx, "alex", "nowhere", time.Time{}, DefaultUsageLimits, func(Usage) {}); KindOf(err) != KindNotFound {
		t.Errorf("a folder that is not there: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	usageHook = func(string) { cancel() }
	defer func() { usageHook = nil }()
	if _, err := s.Usage(cctx, "alex", "Search", time.Time{}, DefaultUsageLimits, func(Usage) {}); KindOf(err) != KindCanceled {
		t.Errorf("canceled: %v", err)
	}
}
