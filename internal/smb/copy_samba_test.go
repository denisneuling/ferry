package smb

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A copy the request abandons between two chunks leaves neither the target
// nor its temporary file behind.
func TestCopyAbandoned(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	dir := "copy-abandoned-" + time.Now().Format("150405.000")
	if _, err := s.Mkdir(ctx, "alex", dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Remove(context.Background(), "alex", dir, true, 100) })
	big := strings.Repeat("x", 3*UploadChunk)
	if _, err := s.Upload(ctx, "alex", dir+"/big.bin", strings.NewReader(big), false, 0); err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(ctx)
	_, err := s.CopyFile(cctx, "alex", dir+"/big.bin", dir+"/copy.bin", time.Time{}, false, func(int64) { cancel() })
	if KindOf(err) != KindCanceled {
		t.Fatalf("an abandoned copy: %v", err)
	}
	left, err := s.ReadDir(ctx, "alex", dir, 100)
	if err != nil || len(left) != 1 || left[0].Name != "big.bin" {
		t.Fatalf("left behind: %+v, %v", left, err)
	}
}

// The plan of a folder: the folder first, then what is in it, bounded.
func TestCopyPlan(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	plan, err := s.CopyPlan(ctx, "alex", "Search/a", time.Time{}, 0)
	if err != nil || !plan[0].Dir || plan[0].Rel != "" || len(plan) != 7 {
		t.Fatalf("plan: %+v, %v", plan, err)
	}
	if _, err := s.CopyPlan(ctx, "alex", "Search/a", time.Time{}, 3); KindOf(err) != KindLimit {
		t.Fatalf("bounded: %v", err)
	}
	// Folders alex may not open make the plan fail rather than copy less.
	if _, err := s.CopyPlan(ctx, "alex", "Search", time.Time{}, 100); KindOf(err) != KindAccessDenied {
		t.Fatalf("Privat in the plan: %v", err)
	}
}
