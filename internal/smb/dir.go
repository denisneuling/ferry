package smb

import (
	"context"
	"errors"
	"io"
	"os"

	smb2 "github.com/cloudsoda/go-smb2"
)

// DirBatch is how many entries Dir.Next asks the server for at once: one
// QUERY_DIRECTORY round trip's worth for ordinary names, and little memory.
const DirBatch = 512

// Dir is an open folder, read in batches.
//
// Like a File, it has a context of its own, detached from the request that
// opened it (call.go), and each batch takes the session's request slot on
// its own: a listing written out to a slow client holds no SMB call while it
// waits, and the session's other requests go in between. The request's end
// stops the listing between batches, never inside one.
type Dir struct {
	f      *smb2.File
	sess   *Session
	parent context.Context    // the opening request's: checked between batches
	cancel context.CancelFunc // ends the folder's own context
	max    int
	n      int
	done   bool
}

// OpenDir opens a folder for listing. It yields at most max entries (a
// bound of ferry's own: Next answers KindLimit beyond it); a file is
// KindNotDir.
func (s *Session) OpenDir(ctx context.Context, share, p string, max int) (_ *Dir, err error) {
	ctx, done := span(ctx, "smb.opendir")
	defer func() { done(err) }()
	sp, err := CleanPath(p)
	if err != nil {
		return nil, err
	}
	dir := sp
	if dir == "" {
		dir = "."
	}
	if err := s.begin(ctx); err != nil {
		return nil, err
	}
	defer s.release()
	dctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	var f *smb2.File
	err = s.watch(cancel, func() error {
		m, err := s.mount(dctx, share)
		if err != nil {
			return err
		}
		if f, err = m.Open(dir); err != nil {
			return s.fail(err)
		}
		fi, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return s.fail(err)
		}
		if !fi.IsDir() {
			_ = f.Close()
			return &Error{Kind: KindNotDir}
		}
		return nil
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return &Dir{f: f, sess: s, parent: ctx, cancel: cancel, max: max}, nil
}

// Next returns the next batch of entries, without "." and "..". At the end
// it returns io.EOF, possibly with a last batch; beyond max entries it
// returns KindLimit, carrying max.
func (d *Dir) Next() ([]Entry, error) {
	if d.done {
		return nil, io.EOF
	}
	var batch []os.FileInfo
	var rerr error
	if err := d.sess.begin(d.parent); err != nil {
		return nil, err
	}
	err := d.sess.watch(d.cancel, func() error {
		batch, rerr = d.f.Readdir(DirBatch)
		return nil
	})
	d.sess.release()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(batch))
	for _, fi := range batch {
		if fi.Name() == "." || fi.Name() == ".." {
			continue
		}
		if d.n == d.max {
			return nil, &Error{Kind: KindLimit, Limit: int64(d.max)}
		}
		d.n++
		out = append(out, Entry{Name: fi.Name(), Dir: fi.IsDir(), Size: fi.Size(), ModTime: fi.ModTime()})
	}
	if errors.Is(rerr, io.EOF) || (rerr == nil && len(batch) == 0) {
		d.done = true
		return out, io.EOF
	}
	if rerr != nil {
		return nil, d.sess.fail(rerr)
	}
	return out, nil
}

// Close closes the folder's handle, under a short deadline of its own, as
// File.Close does: a listing the client left is closed all the same.
func (d *Dir) Close() error {
	defer d.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), closeWait)
	defer cancel()
	if err := d.sess.begin(ctx); err != nil {
		return err
	}
	defer d.sess.release()
	var cerr error
	if err := d.sess.watch(d.cancel, func() error { cerr = d.f.Close(); return nil }); err != nil {
		return err
	}
	if cerr != nil {
		return d.sess.fail(cerr)
	}
	return nil
}
