package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/metapatrol/ferry/internal/cache"
	"github.com/metapatrol/ferry/internal/preview"
	"github.com/metapatrol/ferry/internal/smb"
	"github.com/metapatrol/ferry/internal/thumb"
)

func failureKey(variant string, size int64, mod time.Time) cache.Key {
	return cache.Key{Kind: "thumbnail", Variant: variant, Share: "alex", Path: "Photos/broken.jpg", Size: size, ModTime: mod}
}

// Only what the file decides is remembered; never the moment's answer.
func TestLasting(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{thumb.ErrUnsupported, true},
		{thumb.ErrTooLarge, true},
		{fmt.Errorf("%w: pdfium", preview.ErrUnsupported), true},
		{preview.ErrBlank, true},
		{preview.ErrTooLarge, true},
		{nil, false},
		{preview.ErrTimeout, false},
		{fmt.Errorf("%w: no budget", preview.ErrUnavailable), false},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
		{&smb.Error{Kind: smb.KindUnreachable}, false},
		{errors.New("something else"), false},
	} {
		if got := lasting(c.err); got != c.want {
			t.Errorf("%v: %v", c.err, got)
		}
	}
}

func TestFailures(t *testing.T) {
	f := newFailures()
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return now }
	mod := now.Add(-time.Hour)

	f.put(failureKey("640", 100, mod), thumb.ErrTooLarge)
	f.put(failureKey("320", 100, mod), preview.ErrTimeout) // not remembered
	if err, ok := f.get(failureKey("640", 100, mod)); !ok || !errors.Is(err, thumb.ErrTooLarge) {
		t.Fatalf("640: %v %v", err, ok)
	}
	// Another size, another version of the file: not this answer.
	if _, ok := f.get(failureKey("320", 100, mod)); ok {
		t.Error("the 320 answered by the 640's failure, or by a timeout")
	}
	if _, ok := f.get(failureKey("640", 101, mod)); ok {
		t.Error("a file of another size answered")
	}
	if _, ok := f.get(failureKey("640", 100, mod.Add(time.Second))); ok {
		t.Error("a file changed since answered")
	}

	f.put(failureKey("160", 100, mod), thumb.ErrUnsupported)
	if err, ok := f.get(failureKey("160", 100, mod)); !ok || !errors.Is(err, thumb.ErrUnsupported) {
		t.Fatalf("160: %v %v", err, ok)
	}
	now = now.Add(failureTTL)
	if _, ok := f.get(failureKey("640", 100, mod)); ok {
		t.Error("remembered past its time")
	}
}

func TestFailuresBounded(t *testing.T) {
	f := newFailures()
	f.max = 100
	mod := time.Unix(0, 0)
	for i := range 250 {
		f.put(failureKey("320", int64(i), mod), thumb.ErrUnsupported)
	}
	if n := f.len(); n > 100 {
		t.Fatalf("%d entries, bound 100", n)
	}
	if _, ok := f.get(failureKey("320", 249, mod)); !ok {
		t.Error("the newest forgotten")
	}
	if _, ok := f.get(failureKey("320", 0, mod)); ok {
		t.Error("the oldest kept")
	}
	if len(f.queue) > 101 {
		t.Errorf("the queue holds %d", len(f.queue))
	}
}
