package smb

import (
	"context"
	"errors"
	"time"
)

// UsageLimits bound adding up a folder: how deep it goes, how many folders
// it lists, how many entries it counts in all, and how many one folder may
// have for it to be counted. The time is the context's.
type UsageLimits struct {
	Depth, Folders, Entries, FolderEntries int
}

// DefaultUsageLimits are a folder size's bounds unless the caller says others.
var DefaultUsageLimits = UsageLimits{Depth: 32, Folders: 20000, Entries: 200000, FolderEntries: 200000}

// Usage is what a folder holds, as far as it was counted.
type Usage struct {
	Files, Folders int
	Bytes          int64
	// Skipped: folders the server refused, or too large to count.
	Skipped   int
	Truncated bool
	// Reason is the bound that stopped it: folders, entries, depth or time.
	Reason string
}

// usageHook, when tests set it, runs before each folder Usage lists.
var usageHook func(dir string)

// Usage adds up the files below the folder p — their number and their
// sizes as the server lists them — breadth first, as the folders are now
// or, with at, as that snapshot holds them. It lists folder by folder, as
// Search does, each listing taking the session's request slot only for
// itself; progress gets the count after each folder.
//
// Samba decides what is counted: a folder the server refuses below p is
// skipped and counted as skipped, never an error; p itself refused is one.
// A bound reached, or ctx's deadline, ends it with what was counted, said
// to be truncated.
func (s *Session) Usage(ctx context.Context, share, p string, at time.Time, lim UsageLimits, progress func(Usage)) (Usage, error) {
	var u Usage
	root, err := CleanPath(p)
	if err != nil {
		return u, err
	}
	type folder struct {
		sp    string
		depth int
	}
	queue := []folder{{root, 0}}
	stop := func(reason string) (Usage, error) {
		u.Truncated, u.Reason = true, reason
		return u, nil
	}
	entries := 0
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return stop("time")
		}
		if err := ctx.Err(); err != nil {
			return u, classify(err)
		}
		if u.Folders >= lim.Folders {
			return stop("folders")
		}
		if usageHook != nil {
			usageHook(f.sp)
		}
		list, err := s.listAll(ctx, share, f.sp, at, lim.FolderEntries)
		switch {
		case err == nil:
		case f.sp == root:
			return u, err
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return stop("time")
		case KindOf(err) == KindAccessDenied, KindOf(err) == KindLimit, KindOf(err) == KindNotFound:
			u.Skipped++
			continue
		default:
			return u, err
		}
		if f.sp != root {
			u.Folders++ // the folder counted is one below p
		}
		for _, e := range list {
			if entries == lim.Entries {
				progress(u)
				return stop("entries")
			}
			entries++
			if !e.Dir {
				u.Files++
				u.Bytes += e.Size
				continue
			}
			if f.depth+1 > lim.Depth {
				u.Truncated, u.Reason = true, "depth"
				continue
			}
			child := e.Name
			if f.sp != "" {
				child = f.sp + `\` + e.Name
			}
			queue = append(queue, folder{child, f.depth + 1})
		}
		progress(u)
	}
	return u, nil
}
