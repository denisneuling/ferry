package smb

import (
	"context"
	"errors"
	"io"
	"time"
)

// Copying within a share: through ferry, a chunk at a time, as an upload
// whose bytes come from the share. go-smb2 can ask the server to copy
// (FSCTL_SRV_COPYCHUNK, File.ReadFrom), but it does a whole file in one
// call, with no point between chunks to see that the client left or to
// give the watchdog more time (call.go): a large file would outlast the
// call timeout and end the session. A copy here takes the request slot per
// chunk, stops between chunks when the request ends, and writes through a
// temporary name like an upload, so no file is ever left half-copied under
// its real name.

// CopyItem is one entry a copy makes: its path below the source (empty
// for the source itself, backslash-separated), and what it is.
type CopyItem struct {
	Rel  string
	Dir  bool
	Size int64
}

// CopyPlan lists what copying from makes, as the path is now or, with at,
// as that snapshot holds it: the file, or the folder and everything below
// it, each folder before what is in it. More than max entries is KindLimit
// (max <= 0: no limit).
func (s *Session) CopyPlan(ctx context.Context, share, from string, at time.Time, max int) ([]CopyItem, error) {
	sp, err := CleanPath(from)
	if err != nil {
		return nil, err
	}
	var root Entry
	if at.IsZero() {
		root, err = s.Stat(ctx, share, from)
	} else {
		root, err = s.StatAt(ctx, share, from, at)
	}
	if err != nil {
		return nil, err
	}
	if !root.Dir {
		return []CopyItem{{Size: root.Size}}, nil
	}
	plan := []CopyItem{{Dir: true}}
	type folder struct {
		rel   string
		depth int
	}
	queue := []folder{{"", 0}}
	limit := max
	if limit <= 0 {
		limit = int(^uint(0) >> 1)
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if f.depth > maxDepth {
			return nil, &Error{Kind: KindLimit}
		}
		dir := joinSMB(sp, f.rel)
		entries, err := s.listAll(ctx, share, dir, at, limit)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			rel := joinSMB(f.rel, e.Name)
			plan = append(plan, CopyItem{Rel: rel, Dir: e.Dir, Size: e.Size})
			if max > 0 && len(plan) > max {
				return nil, &Error{Kind: KindLimit, Limit: int64(max)}
			}
			if e.Dir {
				queue = append(queue, folder{rel, f.depth + 1})
			}
		}
	}
	return plan, nil
}

// CopyFile copies one file, as it is now or as the snapshot at at holds
// it, to another path of the same share; progress gets the bytes of each
// chunk written. Without overwrite, a target that exists is KindExists.
func (s *Session) CopyFile(ctx context.Context, share, from, to string, at time.Time, overwrite bool, progress func(n int64)) (Entry, error) {
	f, err := s.open(ctx, share, from, at)
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = f.Close() }()
	r := &copyReader{f: f, progress: progress}
	e, err := s.Upload(ctx, share, to, r, overwrite, 0)
	// Upload reads a failed read as a client that went away; here the
	// reader is the share, and its own error says what happened.
	if KindOf(err) == KindCanceled && r.err != nil && ctx.Err() == nil {
		return Entry{}, r.err
	}
	return e, err
}

type copyReader struct {
	f        *File
	progress func(int64)
	err      error
}

func (c *copyReader) Read(p []byte) (int, error) {
	n, err := c.f.Read(p)
	if n > 0 && c.progress != nil {
		c.progress(int64(n))
	}
	if err != nil && !errors.Is(err, io.EOF) {
		c.err = err
	}
	return n, err
}

// joinSMB joins backslash-separated paths, either of which may be empty.
func joinSMB(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + `\` + b
}
