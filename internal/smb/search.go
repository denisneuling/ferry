package smb

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// SearchLimits bound one search below a folder: how deep it goes, how many
// folders it lists, how many hits it gives, and how many entries one folder
// may have for it to be looked through. The time is the context's.
type SearchLimits struct {
	Depth, Folders, Hits, FolderEntries int
}

// DefaultSearchLimits are a search's bounds unless the caller says others.
var DefaultSearchLimits = SearchLimits{Depth: 32, Folders: 20000, Hits: 1000, FolderEntries: 200000}

// SearchResult says how a search went: hits given, folders listed, folders
// skipped because the server refused them or they were too large, and
// whether it stopped at a bound before looking through everything.
type SearchResult struct {
	Hits, Folders, Skipped int
	Truncated              bool
	// Reason is the bound that stopped it: hits, folders, depth or time.
	Reason string
}

// searchHook, when tests set it, runs before each folder a search lists.
var searchHook func(dir string)

// Search looks for names containing q below the folder p, breadth first,
// as the folders are now or, with at, as that snapshot holds them. There is
// no index: every folder is listed, one at a time, each listing taking the
// session's request slot only for itself, so that the session's other
// requests go in between. emit gets the hits of each folder as they are
// found, with the folder's path (slash-separated, as the API writes it); an
// error from it ends the search.
//
// Names match case-insensitively, after Unicode normalisation (NFC): a name
// written decomposed, as macOS writes it, matches the same name composed.
//
// Samba decides what is searched: a folder the server refuses below p is
// skipped and counted, never an error; p itself refused is one. When ctx's
// deadline passes, or a bound in lim is reached, the result says so and
// what was found stands. A request that ended otherwise is an error, and
// stops the search before the next folder.
func (s *Session) Search(ctx context.Context, share, p, q string, at time.Time, lim SearchLimits, emit func(dir string, hits []Entry) error) (SearchResult, error) {
	var res SearchResult
	root, err := CleanPath(p)
	if err != nil {
		return res, err
	}
	key := searchKey()
	want := key(q)
	if want == "" {
		return res, &Error{Kind: KindInvalidPath}
	}
	type folder struct {
		sp    string
		depth int
	}
	queue := []folder{{root, 0}}
	stop := func(reason string) (SearchResult, error) {
		res.Truncated, res.Reason = true, reason
		return res, nil
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return stop("time")
		}
		if err := ctx.Err(); err != nil {
			return res, classify(err)
		}
		if res.Folders >= lim.Folders {
			return stop("folders")
		}
		if searchHook != nil {
			searchHook(f.sp)
		}
		entries, err := s.listAll(ctx, share, f.sp, at, lim.FolderEntries)
		switch {
		case err == nil:
		case f.sp == root:
			return res, err
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return stop("time")
		case KindOf(err) == KindAccessDenied, KindOf(err) == KindLimit, KindOf(err) == KindNotFound:
			// Refused, too large, or gone since it was listed: the rest
			// of the search goes on without it.
			res.Skipped++
			continue
		default:
			return res, err
		}
		res.Folders++
		dir := slashed(f.sp)
		var hits []Entry
		for _, e := range entries {
			if strings.Contains(key(e.Name), want) {
				if res.Hits+len(hits) == lim.Hits {
					if err := emitHits(emit, dir, hits, &res); err != nil {
						return res, err
					}
					return stop("hits")
				}
				hits = append(hits, e)
			}
			if e.Dir {
				if f.depth+1 > lim.Depth {
					res.Truncated, res.Reason = true, "depth"
					continue
				}
				child := e.Name
				if f.sp != "" {
					child = f.sp + `\` + e.Name
				}
				queue = append(queue, folder{child, f.depth + 1})
			}
		}
		if err := emitHits(emit, dir, hits, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func emitHits(emit func(string, []Entry) error, dir string, hits []Entry, res *SearchResult) error {
	if len(hits) == 0 {
		return nil
	}
	res.Hits += len(hits)
	return emit(dir, hits)
}

// listAll reads one folder whole, at most max entries (KindLimit beyond).
func (s *Session) listAll(ctx context.Context, share, sp string, at time.Time, max int) ([]Entry, error) {
	p := slashed(sp)
	var d *Dir
	var err error
	if at.IsZero() {
		d, err = s.OpenDir(ctx, share, p, max)
	} else {
		d, err = s.OpenDirAt(ctx, share, p, max, at)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()
	var out []Entry
	for {
		batch, err := d.Next()
		out = append(out, batch...)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// searchKey is how a name is compared: composed (NFC) and case-folded.
func searchKey() func(string) string {
	fold := cases.Fold()
	return func(s string) string { return fold.String(norm.NFC.String(s)) }
}
