package smb

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
)

// TreeEntry is one entry of a tree Tree lists.
type TreeEntry struct {
	// Path is below the share's root, slash-separated.
	Path    string
	Dir     bool
	Size    int64
	ModTime time.Time
}

// Tree lists the entries at paths and everything below those that are
// folders, each folder before what it holds: what an archive of them
// contains. It visits at most max entries (KindLimit beyond, before the
// caller has done anything with them) and stops at the first error — a
// folder Samba refuses to list is an error, never left out. The share's
// root is not a path Tree takes.
func (s *Session) Tree(ctx context.Context, share string, paths []string, max int) ([]TreeEntry, error) {
	c, err := s.start(ctx, "smb.tree")
	if err != nil {
		return nil, err
	}
	defer c.end()
	m, err := s.mount(c.ctx, share)
	if err != nil {
		return nil, err
	}
	var out []TreeEntry
	for _, p := range paths {
		sp, err := CleanPath(p)
		if err != nil {
			return nil, err
		}
		if sp == "" {
			return nil, &Error{Kind: KindInvalidPath}
		}
		if err := c.canceled(); err != nil {
			return nil, err
		}
		e, err := s.stat(m, sp)
		if err != nil {
			return nil, err
		}
		out = append(out, TreeEntry{Path: slashed(sp), Dir: e.Dir, Size: e.Size, ModTime: e.ModTime})
		if len(out) > max {
			return nil, &Error{Kind: KindLimit, Limit: int64(max)}
		}
		if e.Dir {
			if err := s.tree(c, m, sp, 0, max, &out); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// tree appends everything below dir, each folder before what it holds.
func (s *Session) tree(c *call, m *smb2.Share, dir string, depth, max int, out *[]TreeEntry) error {
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
	var folders []string
	for {
		batch, rerr := f.Readdir(256)
		for _, fi := range batch {
			if fi.Name() == "." || fi.Name() == ".." {
				continue
			}
			child := dir + `\` + fi.Name()
			*out = append(*out, TreeEntry{Path: slashed(child), Dir: fi.IsDir(), Size: size(fi), ModTime: fi.ModTime()})
			if fi.IsDir() {
				folders = append(folders, child)
			}
		}
		if len(*out) > max {
			_ = f.Close()
			return &Error{Kind: KindLimit, Limit: int64(max)}
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
	for _, sub := range folders {
		if err := s.tree(c, m, sub, depth+1, max, out); err != nil {
			return err
		}
	}
	return nil
}

func slashed(sp string) string { return strings.ReplaceAll(sp, `\`, "/") }
