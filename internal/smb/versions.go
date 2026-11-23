package smb

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
)

// Previous versions: what a server keeps of a path in its snapshots, read
// through SMB's own means (Samba's vfs_shadow_copy2, Windows' shadow
// copies), never through a snapshot directory of one file system. A server
// without snapshots offers none, and nothing here asks for more.
//
// Two SMB requests, from ferry's copy of go-smb2 (third_party/go-smb2): the
// snapshot list for a path (FSCTL_SRV_ENUMERATE_SNAPSHOTS), and an open
// that reads a path as it was at one of them (the TWrp create context). A
// snapshot is read-only; nothing here writes.
//
// The two belong together. When snapshot names carry more than the time
// (shadow:snapprefix, shadow:delimiter: "autosnap_2026-11-23_15:00:09_hourly"),
// Samba cannot derive the name from the time in the open and resolves it
// against the list it last enumerated on the connection; after an
// enumeration of another dataset, or none, the open fails. So every open at
// a time enumerates the same path first, in the same call: the call holds
// the session's request slot (call.go), and a connection serves one request
// at a time, so no other enumeration can come between the two.

// TokenLayout is a snapshot's time as SMB names it ("@GMT-…", UTC).
const TokenLayout = "@GMT-2006.01.02-15.04.05"

// MaxVersions bounds how many snapshots one listing of versions opens.
const MaxVersions = 500

// ParseToken reads a snapshot token exactly as TokenLayout writes it, and
// refuses anything else: another length, another form, a time that does
// not write back to the same token.
func ParseToken(s string) (time.Time, error) {
	if len(s) != len(TokenLayout) {
		return time.Time{}, &Error{Kind: KindInvalidPath}
	}
	t, err := time.Parse(TokenLayout, s)
	if err != nil || t.Format(TokenLayout) != s {
		return time.Time{}, &Error{Kind: KindInvalidPath}
	}
	return t, nil
}

// Token writes t as a snapshot token.
func Token(t time.Time) string { return t.UTC().Format(TokenLayout) }

// Version is a path as one snapshot holds it.
type Version struct {
	At      time.Time // the snapshot's time, UTC
	Dir     bool
	Size    int64
	ModTime time.Time
}

// Versions lists what the server's snapshots hold of a path, newest
// first: one Version for each state that differs from the one after it
// and from the path as it is now, at the newest snapshot holding that
// state. Snapshots in which the path does not exist are left out. At most
// max snapshots are looked at, the newest. A server without snapshots
// gives none.
func (s *Session) Versions(ctx context.Context, share, p string, max int) ([]Version, error) {
	sp, err := CleanPath(p)
	if err != nil {
		return nil, err
	}
	c, err := s.start(ctx, "smb.versions")
	if err != nil {
		return nil, err
	}
	defer c.end()
	m, err := s.mount(c.ctx, share)
	if err != nil {
		return nil, err
	}
	now, nowErr := s.stat(m, sp)
	if nowErr != nil && KindOf(nowErr) != KindNotFound {
		return nil, nowErr
	}
	tokens, err := s.snapshots(m, sp)
	if err != nil {
		return nil, err
	}
	if len(tokens) > max {
		tokens = tokens[:max]
	}
	var out []Version
	last := Version{}
	if nowErr == nil {
		last = Version{Dir: now.Dir, Size: now.Size, ModTime: now.ModTime}
	}
	for _, tok := range tokens {
		if err := c.canceled(); err != nil {
			return nil, err
		}
		c.kick()
		at, err := ParseToken(tok)
		if err != nil {
			continue // a token of a form ferry does not read: not offered
		}
		v, err := s.statAt(m, sp, at)
		if KindOf(err) == KindNotFound {
			last = Version{}
			continue
		}
		if err != nil {
			return nil, err
		}
		if v.Dir == last.Dir && v.Size == last.Size && v.ModTime.Equal(last.ModTime) {
			continue
		}
		last = v
		out = append(out, v)
	}
	return out, nil
}

// snapshots enumerates the snapshots for a path, newest first. A path that
// does not exist now is enumerated at its nearest folder that does: the
// same dataset, as a nested one has its root in the share as it is now.
func (s *Session) snapshots(m *smb2.Share, sp string) ([]string, error) {
	for {
		name := sp
		if name == "" {
			name = "."
		}
		f, err := m.Open(name)
		if errors.Is(err, os.ErrNotExist) && sp != "" {
			sp = parentOf(sp)
			continue
		}
		if err != nil {
			return nil, s.fail(err)
		}
		tokens, err := f.Snapshots()
		_ = f.Close()
		if noSnapshots(err) {
			return nil, nil
		}
		if err != nil {
			return nil, s.fail(err)
		}
		return tokens, nil
	}
}

// openAt opens a path as it was at a snapshot, after enumerating the same
// path (see above); a time that is not one of its snapshots is KindNotFound.
// The caller holds the request slot.
func (s *Session) openAt(m *smb2.Share, sp string, at time.Time) (*smb2.File, error) {
	tokens, err := s.snapshots(m, sp)
	if err != nil {
		return nil, err
	}
	want := Token(at)
	for _, t := range tokens {
		if t == want {
			return s.openToken(m, sp, at)
		}
	}
	return nil, &Error{Kind: KindNotFound}
}

// openToken opens at a time already enumerated on this connection.
func (s *Session) openToken(m *smb2.Share, sp string, at time.Time) (*smb2.File, error) {
	name := sp
	if name == "" {
		name = "."
	}
	f, err := m.OpenAt(name, at)
	if err != nil {
		return nil, s.fail(err)
	}
	return f, nil
}

// statAt is the entry at a time already enumerated on this connection.
func (s *Session) statAt(m *smb2.Share, sp string, at time.Time) (Version, error) {
	f, err := s.openToken(m, sp, at)
	if err != nil {
		return Version{}, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return Version{}, s.fail(err)
	}
	return Version{At: at, Dir: fi.IsDir(), Size: size(fi), ModTime: fi.ModTime()}, nil
}

// noSnapshots tells the one answer that means a share keeps no snapshots:
// Samba without shadow_copy2 refuses the request as one it does not know,
// rather than listing none. Every other failure is one.
func noSnapshots(err error) bool {
	var re *smb2.ResponseError
	return errors.As(err, &re) && re.Code == statusInvalidDeviceRequest
}

func parentOf(sp string) string {
	if i := strings.LastIndexByte(sp, '\\'); i >= 0 {
		return sp[:i]
	}
	return ""
}
