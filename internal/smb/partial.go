package smb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudsoda/go-smb2"
)

// An upload in pieces: a file written over several requests, which a client
// can go on with after a broken connection or a reload. Its bytes go into a
// partial file beside the target, `.ferry-partial-<id>`, which becomes the
// target only when it is complete. The id is 128 random bits, made here and
// known only to the client that began the upload; the partial's name is
// made from the id alone, never from anything else the client sends. ferry
// keeps no state: where an upload has come to is the partial's size on the
// share, and Samba decides who may read or write it as for any file. The
// leading dot hides it from file managers that hide dotfiles, and from
// Windows by Samba's default `hide dot files`.

// PartialPrefix starts every partial file's name.
const PartialPrefix = ".ferry-partial-"

var partialID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidPartialID says whether id is one PartialBegin could have made.
func ValidPartialID(id string) bool { return partialID.MatchString(id) }

// partialPath is the partial of upload id for the target sp: in the
// target's folder, named by the id.
func partialPath(sp, id string) string {
	name := PartialPrefix + id
	if i := strings.LastIndexByte(sp, '\\'); i >= 0 {
		return sp[:i+1] + name
	}
	return name
}

// uploading holds the uploads in pieces with a step under way in this
// process, by share and partial path. Checking the partial's size and
// writing at it are two calls: two requests for the same upload — two tabs,
// two devices — could both find the same size and both write. Each step
// therefore holds its upload for itself, and a second one at the same time
// is KindBusy, to be tried again. ferry runs as one replica (the chart's
// rule: sessions live in memory), so a lock in the process is enough.
var uploading = struct {
	sync.Mutex
	busy map[string]bool
}{busy: map[string]bool{}}

// hold takes upload key for one step, or says it is taken.
func hold(key string) (func(), error) {
	uploading.Lock()
	defer uploading.Unlock()
	if uploading.busy[key] {
		return nil, &Error{Kind: KindBusy}
	}
	uploading.busy[key] = true
	return func() {
		uploading.Lock()
		delete(uploading.busy, key)
		uploading.Unlock()
	}, nil
}

// partial checks an upload's target and id and opens its share.
func (s *Session) partial(ctx context.Context, share, p, id string) (sp, pp string, err error) {
	if !ValidPartialID(id) {
		return "", "", &Error{Kind: KindInvalidPath}
	}
	if sp, err = CleanPath(p); err != nil {
		return "", "", err
	}
	if sp == "" {
		return "", "", &Error{Kind: KindIsDir}
	}
	return sp, partialPath(sp, id), nil
}

// PartialBegin starts an upload in pieces to p: an empty partial file,
// after the same check a whole upload makes first — a target that exists
// is KindExists unless overwrite. It returns the upload's id.
func (s *Session) PartialBegin(ctx context.Context, share, p string, overwrite bool) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", &Error{Kind: KindInternal}
	}
	id := hex.EncodeToString(b)
	sp, pp, err := s.partial(ctx, share, p, id)
	if err != nil {
		return "", err
	}
	err = s.fileStep(ctx, share, func(m *smb2.Share) error {
		if err := s.refuseExisting(m, sp, overwrite); err != nil {
			return err
		}
		f, err := m.OpenFile(pp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return s.fail(err)
		}
		if err := f.Close(); err != nil {
			return s.fail(err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// PartialSize is how far upload id has come: its partial's size.
func (s *Session) PartialSize(ctx context.Context, share, p, id string) (int64, error) {
	_, pp, err := s.partial(ctx, share, p, id)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.fileStep(ctx, share, func(m *smb2.Share) error {
		e, err := s.stat(m, pp)
		n = e.Size
		return err
	})
	return n, err
}

// PartialAppend writes the next piece of upload id, read from r: only when
// the partial ends at offset (KindOffset otherwise, and nothing written),
// and never past total bytes in all (KindLimit). Pieces are written one
// after the other, so the partial's size is always where it has come to:
// a piece cut off part-way leaves the bytes that arrived, and the client
// goes on from the size it is then told.
func (s *Session) PartialAppend(ctx context.Context, share, p, id string, offset int64, r io.Reader, total int64) (int64, error) {
	_, pp, err := s.partial(ctx, share, p, id)
	if err != nil {
		return 0, err
	}
	release, err := hold(share + "\x00" + strings.ToLower(pp))
	if err != nil {
		return 0, err
	}
	defer release()
	fctx, fcancel := context.WithCancel(context.WithoutCancel(ctx))
	defer fcancel()
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
			// Read and write: go-smb2 asks for no attribute access with write
			// alone, and the size is read through the handle.
			if f, err = m.OpenFile(pp, os.O_RDWR, 0); err != nil {
				return s.fail(err)
			}
			fi, err := f.Stat()
			if err != nil {
				_ = f.Close()
				return s.fail(err)
			}
			if fi.Size() != offset {
				_ = f.Close()
				return &Error{Kind: KindOffset}
			}
			return nil
		})
	}()
	if err != nil {
		return 0, err
	}
	closeFile := func() {
		cctx, cancel := context.WithTimeout(context.Background(), closeWait)
		defer cancel()
		if s.begin(cctx) != nil {
			return
		}
		defer s.release()
		_ = s.watch(fcancel, func() error { return f.Close() })
	}
	at := offset
	buf := make([]byte, UploadChunk)
	for {
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			if total > 0 && at+int64(n) > total {
				closeFile()
				return at, &Error{Kind: KindLimit, Limit: total}
			}
			if werr := s.writeAt(ctx, fcancel, f, buf[:n], at); werr != nil {
				closeFile()
				return at, werr
			}
			at += int64(n)
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			closeFile()
			return at, &Error{Kind: KindCanceled}
		}
	}
	closeFile()
	return at, nil
}

// writeAt is writeChunk at an offset.
func (s *Session) writeAt(ctx context.Context, fcancel context.CancelFunc, f *smb2.File, b []byte, at int64) error {
	if err := s.begin(ctx); err != nil {
		return err
	}
	defer s.release()
	return s.watch(fcancel, func() error {
		for len(b) > 0 {
			n, err := f.WriteAt(b, at)
			if err != nil {
				return s.fail(err)
			}
			b, at = b[n:], at+int64(n)
		}
		return nil
	})
}

// PartialFinish makes upload id the target: only when its partial holds
// exactly size bytes (KindOffset otherwise), and after the same check as
// at its beginning, made again now.
func (s *Session) PartialFinish(ctx context.Context, share, p, id string, size int64, overwrite bool) (Entry, error) {
	sp, pp, err := s.partial(ctx, share, p, id)
	if err != nil {
		return Entry{}, err
	}
	release, err := hold(share + "\x00" + strings.ToLower(pp))
	if err != nil {
		return Entry{}, err
	}
	defer release()
	var e Entry
	err = s.fileStep(ctx, share, func(m *smb2.Share) error {
		got, err := s.stat(m, pp)
		if err != nil {
			return err
		}
		if got.Size != size {
			return &Error{Kind: KindOffset}
		}
		if err := s.refuseExisting(m, sp, overwrite); err != nil {
			return err
		}
		// go-smb2 renames with ReplaceIfExists: an existing target is
		// replaced in the one call, never removed first, so there is no
		// moment without it. That the target may be replaced at all is
		// refuseExisting's check just before.
		if err := m.Rename(pp, sp); err != nil {
			return s.fail(err)
		}
		e, err = s.stat(m, sp)
		return err
	})
	return e, err
}

// PartialDiscard removes upload id's partial: the client gave it up.
func (s *Session) PartialDiscard(ctx context.Context, share, p, id string) error {
	_, pp, err := s.partial(ctx, share, p, id)
	if err != nil {
		return err
	}
	release, err := hold(share + "\x00" + strings.ToLower(pp))
	if err != nil {
		return err
	}
	defer release()
	return s.fileStep(ctx, share, func(m *smb2.Share) error {
		if err := m.Remove(pp); err != nil {
			return s.fail(err)
		}
		return nil
	})
}

// fileStep runs one step on the share in the session's slot, under a
// context the request does not reach and the call watchdog, as the steps
// of a whole upload are.
func (s *Session) fileStep(ctx context.Context, share string, step func(m *smb2.Share) error) error {
	fctx, fcancel := context.WithCancel(context.WithoutCancel(ctx))
	defer fcancel()
	if err := s.begin(ctx); err != nil {
		return err
	}
	defer s.release()
	return s.watch(fcancel, func() error {
		m, err := s.mount(fctx, share)
		if err != nil {
			return err
		}
		return step(m)
	})
}
