package smb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	smb2 "github.com/cloudsoda/go-smb2"
)

// The operations that change a share. Each is a call (call.go): the request
// slot for its whole run, go-smb2 under a context the request does not
// reach.

// Stat returns the entry at a path. The share's root is a folder named "".
func (s *Session) Stat(ctx context.Context, share, p string) (Entry, error) {
	sp, err := CleanPath(p)
	if err != nil {
		return Entry{}, err
	}
	c, err := s.start(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer c.end()
	m, err := s.mount(c.ctx, share)
	if err != nil {
		return Entry{}, err
	}
	return s.stat(m, sp)
}

func (s *Session) stat(m *smb2.Share, sp string) (Entry, error) {
	name := sp
	if name == "" {
		name = "."
	}
	fi, err := m.Stat(name)
	if err != nil {
		return Entry{}, s.fail(err)
	}
	return Entry{Name: base(sp), Dir: fi.IsDir(), Size: size(fi), ModTime: fi.ModTime()}, nil
}

// Mkdir creates a folder; one that exists is KindExists.
func (s *Session) Mkdir(ctx context.Context, share, p string) (Entry, error) {
	sp, err := CleanPath(p)
	if err != nil {
		return Entry{}, err
	}
	if sp == "" {
		return Entry{}, &Error{Kind: KindExists}
	}
	c, err := s.start(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer c.end()
	m, err := s.mount(c.ctx, share)
	if err != nil {
		return Entry{}, err
	}
	if err := m.Mkdir(sp, 0o755); err != nil {
		return Entry{}, s.fail(err)
	}
	return s.stat(m, sp)
}

// Remove deletes a file, or a folder: an empty one, or with recursive
// everything in it, depth first. A recursive delete visits at most max
// entries (KindLimit beyond, before anything is deleted) and stops at the
// first error, leaving what it had not reached.
func (s *Session) Remove(ctx context.Context, share, p string, recursive bool, max int) error {
	sp, err := CleanPath(p)
	if err != nil {
		return err
	}
	if sp == "" {
		// The share's root is not deleted through ferry, recursively or not.
		return &Error{Kind: KindAccessDenied}
	}
	c, err := s.start(ctx)
	if err != nil {
		return err
	}
	defer c.end()
	m, err := s.mount(c.ctx, share)
	if err != nil {
		return err
	}
	e, err := s.stat(m, sp)
	if err != nil {
		return err
	}
	if !e.Dir || !recursive {
		if err := m.Remove(sp); err != nil {
			return s.fail(err)
		}
		return nil
	}
	// Collect first, so that a tree beyond the bound is refused whole rather
	// than deleted in part.
	var order []string
	if err := s.walk(c, m, sp, 0, max, &order); err != nil {
		return err
	}
	for _, q := range order {
		if err := c.canceled(); err != nil {
			return err
		}
		c.kick()
		if err := m.Remove(q); err != nil {
			return s.fail(err)
		}
	}
	return nil
}

// maxDepth bounds a recursive walk, whatever the server's tree looks like.
const maxDepth = 64

// walk appends every path below dir, children before their folder, and dir
// itself last.
func (s *Session) walk(c *call, m *smb2.Share, dir string, depth, max int, order *[]string) error {
	if depth > maxDepth {
		return &Error{Kind: KindLimit}
	}
	if err := c.canceled(); err != nil {
		return err
	}
	c.kick()
	f, err := m.Open(dir)
	if err != nil {
		return s.fail(err)
	}
	var children []os.FileInfo
	for {
		batch, rerr := f.Readdir(256)
		children = append(children, batch...)
		if len(*order)+len(children) > max {
			_ = f.Close()
			return &Error{Kind: KindLimit}
		}
		if errors.Is(rerr, io.EOF) || (rerr == nil && len(batch) == 0) {
			break
		}
		if rerr != nil {
			_ = f.Close()
			return s.fail(rerr)
		}
	}
	_ = f.Close()
	for _, fi := range children {
		if fi.Name() == "." || fi.Name() == ".." {
			continue
		}
		child := dir + `\` + fi.Name()
		if fi.IsDir() {
			if err := s.walk(c, m, child, depth+1, max, order); err != nil {
				return err
			}
			continue
		}
		*order = append(*order, child)
	}
	*order = append(*order, dir)
	if len(*order) > max {
		return &Error{Kind: KindLimit}
	}
	return nil
}

// Rename moves an entry within a share. Without overwrite, a target that
// exists is KindExists.
//
// go-smb2 renames with ReplaceIfExists always, so "without overwrite" is a
// check before the rename, not an atomic refusal: a file created at the
// target in the moment between the two is replaced. Worth an option upstream.
func (s *Session) Rename(ctx context.Context, share, from, to string, overwrite bool) (Entry, error) {
	sf, err := CleanPath(from)
	if err != nil {
		return Entry{}, err
	}
	st, err := CleanPath(to)
	if err != nil {
		return Entry{}, err
	}
	if sf == "" || st == "" {
		return Entry{}, &Error{Kind: KindInvalidPath}
	}
	if sf == st {
		return Entry{}, &Error{Kind: KindExists}
	}
	// A folder cannot move into itself; the server's answer to that varies,
	// so it is refused here.
	if strings.HasPrefix(strings.ToLower(st)+`\`, strings.ToLower(sf)+`\`) {
		return Entry{}, &Error{Kind: KindInvalidPath}
	}
	c, err := s.start(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer c.end()
	m, err := s.mount(c.ctx, share)
	if err != nil {
		return Entry{}, err
	}
	if _, err := s.stat(m, sf); err != nil {
		return Entry{}, err
	}
	if err := s.refuseExisting(m, st, overwrite); err != nil {
		return Entry{}, err
	}
	if err := m.Rename(sf, st); err != nil {
		return Entry{}, s.fail(err)
	}
	return s.stat(m, st)
}

// refuseExisting is KindExists when sp exists and may not be replaced, and
// KindIsDir when it is a folder, which is never replaced.
func (s *Session) refuseExisting(m *smb2.Share, sp string, overwrite bool) error {
	e, err := s.stat(m, sp)
	switch {
	case KindOf(err) == KindNotFound:
		return nil
	case err != nil:
		return err
	case e.Dir:
		return &Error{Kind: KindIsDir}
	case !overwrite:
		return &Error{Kind: KindExists}
	}
	return nil
}

// UploadChunk is the size of the writes Upload makes: large writes, for the
// same reason File.WriteTo reads large (StreamChunk).
const UploadChunk = 1 << 20

// Upload writes r to a file. The bytes go to a temporary name in the target
// folder first and are renamed to the real name only once complete, so a
// failed, interrupted or oversized upload leaves no partial file under the
// real name; the temporary file is removed on failure. More than max bytes
// is KindLimit (max <= 0: no limit). An error reading r — the client went
// away — is KindCanceled.
//
// The request slot is taken per write, not for the whole upload: a slow
// client must not hold up the same person's browsing.
func (s *Session) Upload(ctx context.Context, share, p string, r io.Reader, overwrite bool, max int64) (Entry, error) {
	sp, err := CleanPath(p)
	if err != nil {
		return Entry{}, err
	}
	if sp == "" {
		return Entry{}, &Error{Kind: KindIsDir}
	}
	tmp := tempName(sp)
	// The temporary file's own context, which the request's end does not
	// reach (call.go): an upload the client aborts can still close and
	// remove it, and no write is cut off in the middle.
	fctx, fcancel := context.WithCancel(context.WithoutCancel(ctx))
	defer fcancel()

	// Fail early, before the body is read, when the target may not be
	// written; then create the temporary file.
	var f *smb2.File
	err = func() error {
		if err := s.begin(ctx); err != nil {
			return err
		}
		defer s.release()
		return s.watch(fcancel, func() error {
			m, err := s.mount(fctx, share)
			if err != nil {
				return err
			}
			if err := s.refuseExisting(m, sp, overwrite); err != nil {
				return err
			}
			if f, err = m.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err != nil {
				return s.fail(err)
			}
			return nil
		})
	}()
	if err != nil {
		return Entry{}, err
	}

	cleanup := func() {
		// The slot under a fresh context: the request's may be what ended
		// the upload.
		cctx, cancel := context.WithTimeout(context.Background(), closeWait)
		defer cancel()
		if s.begin(cctx) != nil {
			return
		}
		defer s.release()
		_ = s.watch(fcancel, func() error {
			_ = f.Close()
			if m, err := s.mount(fctx, share); err == nil {
				_ = m.Remove(tmp)
			}
			return nil
		})
	}

	buf := make([]byte, UploadChunk)
	var written int64
	for {
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			written += int64(n)
			if max > 0 && written > max {
				cleanup()
				return Entry{}, &Error{Kind: KindLimit}
			}
			if werr := s.writeChunk(ctx, fcancel, f, buf[:n]); werr != nil {
				cleanup()
				return Entry{}, werr
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			cleanup()
			return Entry{}, &Error{Kind: KindCanceled}
		}
	}

	e, err := s.finishUpload(ctx, fctx, fcancel, share, f, tmp, sp, overwrite)
	if err != nil {
		cleanup()
		return Entry{}, err
	}
	return e, nil
}

// finishUpload closes the temporary file and renames it to the real name,
// checking once more that the target may be written.
func (s *Session) finishUpload(ctx, fctx context.Context, fcancel context.CancelFunc, share string, f *smb2.File, tmp, sp string, overwrite bool) (Entry, error) {
	if err := s.begin(ctx); err != nil {
		return Entry{}, err
	}
	defer s.release()
	var e Entry
	err := s.watch(fcancel, func() error {
		if err := f.Close(); err != nil {
			return s.fail(err)
		}
		m, err := s.mount(fctx, share)
		if err != nil {
			return err
		}
		if err := s.refuseExisting(m, sp, overwrite); err != nil {
			return err
		}
		if err := m.Rename(tmp, sp); err != nil {
			return s.fail(err)
		}
		e, err = s.stat(m, sp)
		return err
	})
	return e, err
}

// writeChunk writes b to the temporary file: the slot under the request's
// context — an upload whose client went away stops here — and the writes
// under the file's own, watched.
func (s *Session) writeChunk(ctx context.Context, fcancel context.CancelFunc, f *smb2.File, b []byte) error {
	if err := s.begin(ctx); err != nil {
		return err
	}
	defer s.release()
	return s.watch(fcancel, func() error {
		for len(b) > 0 {
			n, err := f.Write(b)
			if err != nil {
				return s.fail(err)
			}
			b = b[n:]
		}
		return nil
	})
}

// tempName is the temporary name of an upload, in the target's folder. It
// does not repeat the file's name, so it never exceeds the component bound,
// and it starts with a dot so file managers that hide dotfiles hide it.
func tempName(sp string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	name := ".ferry-upload-" + hex.EncodeToString(b)
	if i := strings.LastIndexByte(sp, '\\'); i >= 0 {
		return sp[:i+1] + name
	}
	return name
}

func base(sp string) string {
	if i := strings.LastIndexByte(sp, '\\'); i >= 0 {
		return sp[i+1:]
	}
	return sp
}

func size(fi os.FileInfo) int64 {
	if fi.IsDir() {
		return 0
	}
	return fi.Size()
}
