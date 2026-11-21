package smb

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

// scratch makes a folder of its own for a test in alex's share and removes
// it afterwards.
func scratch(t *testing.T, s *Session) string {
	t.Helper()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "test-" + t.Name() + "-" + hex.EncodeToString(b)
	dir = strings.NewReplacer("/", "-").Replace(dir)
	if _, err := s.Mkdir(context.Background(), "alex", dir); err != nil {
		t.Fatalf("scratch folder: %v", err)
	}
	t.Cleanup(func() { _ = s.Remove(context.Background(), "alex", dir, true, 10000) })
	return dir
}

func names(t *testing.T, s *Session, dir string) string {
	t.Helper()
	entries, err := s.ReadDir(context.Background(), "alex", dir, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range entries {
		n = append(n, e.Name)
	}
	return strings.Join(sortStrings(n), ",")
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

func read(t *testing.T, s *Session, p string) string {
	t.Helper()
	f, err := s.Open(context.Background(), "alex", p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpload(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	dir := scratch(t, s)

	// Larger than one chunk, not a multiple of it.
	big := bytes.Repeat([]byte("ferry "), UploadChunk/2)
	e, err := s.Upload(ctx, "alex", dir+"/big.txt", bytes.NewReader(big), false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "big.txt" || e.Size != int64(len(big)) || e.Dir {
		t.Fatalf("entry %+v", e)
	}
	if got := read(t, s, dir+"/big.txt"); got != string(big) {
		t.Fatalf("content differs: %d bytes, want %d", len(got), len(big))
	}

	if _, err := s.Upload(ctx, "alex", dir+"/big.txt", strings.NewReader("x"), false, 0); KindOf(err) != KindExists {
		t.Fatalf("second upload without overwrite: %v", err)
	}
	if _, err := s.Upload(ctx, "alex", dir+"/big.txt", strings.NewReader("replaced"), true, 0); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := read(t, s, dir+"/big.txt"); got != "replaced" {
		t.Fatalf("after overwrite %q", got)
	}
	if _, err := s.Upload(ctx, "alex", dir+"/empty.txt", strings.NewReader(""), false, 0); err != nil {
		t.Fatalf("empty file: %v", err)
	}
	if got := names(t, s, dir); got != "big.txt,empty.txt" {
		t.Fatalf("folder holds %s", got)
	}
}

// failingReader stands for a client that goes away mid-upload.
type failingReader struct{ n int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, errors.New("connection reset")
	}
	n := min(len(p), r.n)
	r.n -= n
	return n, nil
}

// §11: a failed or interrupted upload leaves nothing under the real name,
// and no temporary file either.
func TestUploadLeavesNothingBehind(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	dir := scratch(t, s)

	if _, err := s.Upload(ctx, "alex", dir+"/cut.bin", &failingReader{n: UploadChunk + 10}, false, 0); KindOf(err) != KindCanceled {
		t.Fatalf("interrupted: %v", err)
	}
	if _, err := s.Upload(ctx, "alex", dir+"/huge.bin", bytes.NewReader(make([]byte, 3*UploadChunk)), false, 2*UploadChunk); KindOf(err) != KindLimit {
		t.Fatalf("over the limit: %v", err)
	}
	if got := names(t, s, dir); got != "" {
		t.Fatalf("left behind: %s", got)
	}

	// An interrupted replacement keeps the old file whole.
	if _, err := s.Upload(ctx, "alex", dir+"/keep.txt", strings.NewReader("original"), false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(ctx, "alex", dir+"/keep.txt", &failingReader{n: 100}, true, 0); KindOf(err) != KindCanceled {
		t.Fatalf("interrupted replacement: %v", err)
	}
	if got := read(t, s, dir+"/keep.txt"); got != "original" {
		t.Fatalf("after an interrupted replacement %q", got)
	}
	if got := names(t, s, dir); got != "keep.txt" {
		t.Fatalf("left behind: %s", got)
	}
}

func TestFolderRenameRemove(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	dir := scratch(t, s)

	if e, err := s.Mkdir(ctx, "alex", dir+"/Trip"); err != nil || !e.Dir || e.Name != "Trip" {
		t.Fatalf("mkdir: %+v %v", e, err)
	}
	if _, err := s.Mkdir(ctx, "alex", dir+"/Trip"); KindOf(err) != KindExists {
		t.Fatalf("mkdir twice: %v", err)
	}
	if _, err := s.Mkdir(ctx, "alex", dir+"/no/such/parent"); KindOf(err) != KindNotFound {
		t.Fatalf("mkdir without parent: %v", err)
	}
	for _, n := range []string{"a.txt", "b.txt"} {
		if _, err := s.Upload(ctx, "alex", dir+"/Trip/"+n, strings.NewReader(n), false, 0); err != nil {
			t.Fatal(err)
		}
	}

	// Rename within a folder, then move to another.
	if e, err := s.Rename(ctx, "alex", dir+"/Trip/a.txt", dir+"/Trip/c.txt", false); err != nil || e.Name != "c.txt" {
		t.Fatalf("rename: %+v %v", e, err)
	}
	if _, err := s.Rename(ctx, "alex", dir+"/Trip/c.txt", dir+"/Trip/b.txt", false); KindOf(err) != KindExists {
		t.Fatalf("rename onto an existing file: %v", err)
	}
	if got := read(t, s, dir+"/Trip/b.txt"); got != "b.txt" {
		t.Fatalf("the target was touched: %q", got)
	}
	if _, err := s.Rename(ctx, "alex", dir+"/Trip/c.txt", dir+"/Trip/b.txt", true); err != nil {
		t.Fatalf("rename with overwrite: %v", err)
	}
	if got := read(t, s, dir+"/Trip/b.txt"); got != "a.txt" {
		t.Fatalf("after overwrite %q", got)
	}
	if _, err := s.Rename(ctx, "alex", dir+"/Trip", dir+"/Trip/Inside", false); KindOf(err) != KindInvalidPath {
		t.Fatalf("folder into itself: %v", err)
	}
	if _, err := s.Rename(ctx, "alex", dir+"/nope", dir+"/x", false); KindOf(err) != KindNotFound {
		t.Fatalf("rename missing: %v", err)
	}
	if e, err := s.Rename(ctx, "alex", dir+"/Trip", dir+"/Holiday", false); err != nil || !e.Dir {
		t.Fatalf("rename folder: %+v %v", e, err)
	}

	// Deleting: a folder only when empty, unless recursive.
	if err := s.Remove(ctx, "alex", dir+"/Holiday", false, 100); KindOf(err) != KindNotEmpty {
		t.Fatalf("non-empty folder: %v", err)
	}
	if _, err := s.Mkdir(ctx, "alex", dir+"/Holiday/Deep"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, "alex", dir+"/Holiday", true, 2); KindOf(err) != KindLimit {
		t.Fatalf("beyond the bound: %v", err)
	}
	if got := names(t, s, dir+"/Holiday"); got != "Deep,b.txt" {
		t.Fatalf("a refused recursive delete deleted something: %s", got)
	}
	if err := s.Remove(ctx, "alex", dir+"/Holiday/b.txt", false, 100); err != nil {
		t.Fatalf("file: %v", err)
	}
	if err := s.Remove(ctx, "alex", dir+"/Holiday", true, 100); err != nil {
		t.Fatalf("recursive: %v", err)
	}
	if got := names(t, s, dir); got != "" {
		t.Fatalf("left: %s", got)
	}
	if err := s.Remove(ctx, "alex", dir+"/nope", false, 100); KindOf(err) != KindNotFound {
		t.Fatalf("missing: %v", err)
	}
	if err := s.Remove(ctx, "alex", "/", true, 100); KindOf(err) != KindAccessDenied {
		t.Fatalf("the root: %v", err)
	}
}

// Samba decides: Scans is read-only for alex, and every write is refused
// there, with nothing left behind.
func TestReadOnlyShare(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	if _, err := s.Upload(ctx, "Scans", "new.txt", strings.NewReader("x"), false, 0); KindOf(err) != KindAccessDenied {
		t.Fatalf("upload: %v", err)
	}
	if _, err := s.Mkdir(ctx, "Scans", "New"); KindOf(err) != KindAccessDenied {
		t.Fatalf("mkdir: %v", err)
	}
	if err := s.Remove(ctx, "Scans", "scan-001.txt", false, 10); KindOf(err) != KindAccessDenied {
		t.Fatalf("remove: %v", err)
	}
	if _, err := s.Rename(ctx, "Scans", "scan-001.txt", "x.txt", false); KindOf(err) != KindAccessDenied {
		t.Fatalf("rename: %v", err)
	}
	entries, err := s.ReadDir(ctx, "Scans", "", 100)
	if err != nil || len(entries) != 1 || entries[0].Name != "scan-001.txt" {
		t.Fatalf("Scans changed: %+v %v", entries, err)
	}
}

// cancelAfter stands for a request whose context ends mid-upload, as when
// the client goes away: it cancels after n bytes, then fails like a reset
// connection.
type cancelAfter struct {
	n      int
	cancel context.CancelFunc
}

func (r *cancelAfter) Read(p []byte) (int, error) {
	if r.n <= 0 {
		r.cancel()
		return 0, errors.New("connection reset")
	}
	n := min(len(p), r.n)
	r.n -= n
	return n, nil
}

// An upload whose request ended still removes its temporary file: the
// handle outlives the request's context long enough to be closed.
func TestUploadCanceledLeavesNothingBehind(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	dir := scratch(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := s.Upload(ctx, "alex", dir+"/gone.bin", &cancelAfter{n: UploadChunk + 1, cancel: cancel}, false, 0); err == nil {
		t.Fatal("a canceled upload succeeded")
	}
	if got := names(t, s, dir); got != "" {
		t.Fatalf("left behind: %s", got)
	}
}

// A download the client aborts closes its handle on the server. The read
// handle does not share delete access, so a leaked one would make the file
// busy.
func TestAbortedDownloadClosesItsHandle(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	dir := scratch(t, s)
	if _, err := s.Upload(context.Background(), "alex", dir+"/video.bin", bytes.NewReader(make([]byte, 3*StreamChunk)), false, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f, err := s.Open(ctx, "alex", dir+"/video.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Read(make([]byte, StreamChunk)); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := f.Read(make([]byte, StreamChunk)); err == nil {
		t.Fatal("read on after the request ended")
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close after the request ended: %v", err)
	}
	if err := s.Remove(context.Background(), "alex", dir+"/video.bin", false, 10); err != nil {
		t.Fatalf("the handle stayed open: %v", err)
	}
}
