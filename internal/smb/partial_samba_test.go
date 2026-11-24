package smb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// An upload in pieces: begun, written in two pieces, a piece at the wrong
// place refused, asked where it has come to, finished — and one given up.
func TestPartialUpload(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	dir := scratch(t, s)
	target := dir + "/film.bin"
	first := bytes.Repeat([]byte("a"), UploadChunk+7) // more than one chunk, not a multiple
	second := []byte("the end")

	id, err := s.PartialBegin(ctx, "alex", target, false)
	if err != nil || !ValidPartialID(id) {
		t.Fatalf("begin: %q, %v", id, err)
	}
	if got := names(t, s, dir); got != PartialPrefix+id {
		t.Fatalf("folder holds %q", got)
	}
	if n, err := s.PartialAppend(ctx, "alex", target, id, 0, bytes.NewReader(first), 0); err != nil || n != int64(len(first)) {
		t.Fatalf("first piece: %d, %v", n, err)
	}
	// A piece repeated, as after a lost answer: refused, nothing written.
	if _, err := s.PartialAppend(ctx, "alex", target, id, 0, bytes.NewReader(first), 0); KindOf(err) != KindOffset {
		t.Fatalf("a piece at the wrong place: %v", err)
	}
	if n, err := s.PartialSize(ctx, "alex", target, id); err != nil || n != int64(len(first)) {
		t.Fatalf("size: %d, %v", n, err)
	}
	total := int64(len(first) + len(second))
	if _, err := s.PartialAppend(ctx, "alex", target, id, int64(len(first)), bytes.NewReader(second), total-1); KindOf(err) != KindLimit {
		t.Fatalf("past the bound: %v", err)
	}
	if _, err := s.PartialAppend(ctx, "alex", target, id, int64(len(first)), bytes.NewReader(second), total); err != nil {
		t.Fatalf("second piece: %v", err)
	}
	if _, err := s.PartialFinish(ctx, "alex", target, id, total+1, false); KindOf(err) != KindOffset {
		t.Fatalf("finished short: %v", err)
	}
	e, err := s.PartialFinish(ctx, "alex", target, id, total, false)
	if err != nil || e.Size != total || e.Name != "film.bin" {
		t.Fatalf("finish: %+v, %v", e, err)
	}
	if got := read(t, s, target); got != string(first)+string(second) {
		t.Fatalf("content: %d bytes", len(got))
	}
	if got := names(t, s, dir); got != "film.bin" {
		t.Fatalf("after finishing the folder holds %q", got)
	}

	// The target exists now: a new upload to it needs overwrite.
	if _, err := s.PartialBegin(ctx, "alex", target, false); KindOf(err) != KindExists {
		t.Fatalf("begin over an existing file: %v", err)
	}
	id, err = s.PartialBegin(ctx, "alex", target, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PartialAppend(ctx, "alex", target, id, 0, strings.NewReader("half"), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.PartialDiscard(ctx, "alex", target, id); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if got := names(t, s, dir); got != "film.bin" {
		t.Fatalf("after discarding the folder holds %q", got)
	}
	// Ids are the server's own: anything else names no file.
	for _, bad := range []string{"", "../x", strings.Repeat("A", 32), strings.Repeat("0", 31), id + "0"} {
		if _, err := s.PartialSize(ctx, "alex", target, bad); KindOf(err) != KindInvalidPath {
			t.Errorf("id %q: %v", bad, err)
		}
	}
}
