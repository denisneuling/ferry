package smb

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"
)

// search runs a search and returns its hits as paths, sorted.
func search(t *testing.T, s *Session, ctx context.Context, share, p, q string, at time.Time, lim SearchLimits) ([]string, SearchResult, error) {
	t.Helper()
	var got []string
	res, err := s.Search(ctx, share, p, q, at, lim, func(dir string, hits []Entry) error {
		for _, e := range hits {
			got = append(got, dir+"/"+e.Name)
		}
		return nil
	})
	sort.Strings(got)
	return got, res, err
}

// The tree hack/samba writes under alex's Search folder.
func TestSearch(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	got, res, err := search(t, s, ctx, "alex", "Search", "rechnung", time.Time{}, DefaultSearchLimits)
	want := []string{
		"Search/Rechnung 2025.pdf",
		"Search/a/b/Grün Rechnung.txt",
		"Search/a/b/c/RECHNUNG.md",
		"Search/a/rechnung strom.txt",
	}
	if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("hits %q, %v", got, err)
	}
	// Privat is alex's to see but not to open: skipped and counted, its
	// hit not given, and no error.
	if res.Hits != 4 || res.Skipped != 1 || res.Folders != 5 || res.Truncated {
		t.Fatalf("result %+v", res)
	}
	// A composed query finds the name macOS wrote decomposed.
	if got, _, err := search(t, s, ctx, "alex", "Search", "GRÜN", time.Time{}, DefaultSearchLimits); err != nil || len(got) != 1 {
		t.Fatalf("Grün: %q, %v", got, err)
	}
	if got, _, err := search(t, s, ctx, "alex", "Search", "nothing like it", time.Time{}, DefaultSearchLimits); err != nil || len(got) != 0 {
		t.Fatalf("no hit: %q, %v", got, err)
	}
}

func TestSearchBounds(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	lim := DefaultSearchLimits
	lim.Hits = 2
	if got, res, err := search(t, s, ctx, "alex", "Search", "rechnung", time.Time{}, lim); err != nil || len(got) != 2 || !res.Truncated || res.Reason != "hits" {
		t.Errorf("two hits: %q, %+v, %v", got, res, err)
	}
	lim = DefaultSearchLimits
	lim.Depth = 1
	if got, res, err := search(t, s, ctx, "alex", "Search", "rechnung", time.Time{}, lim); err != nil || len(got) != 2 || !res.Truncated || res.Reason != "depth" {
		t.Errorf("depth one: %q, %+v, %v", got, res, err)
	}
	lim = DefaultSearchLimits
	lim.Folders = 1
	if got, res, err := search(t, s, ctx, "alex", "Search", "rechnung", time.Time{}, lim); err != nil || len(got) != 1 || !res.Truncated || res.Reason != "folders" {
		t.Errorf("one folder: %q, %+v, %v", got, res, err)
	}
	lim = DefaultSearchLimits
	lim.FolderEntries = 2
	// Search itself has more entries than that: the folder searched is
	// refused as too large.
	if _, _, err := search(t, s, ctx, "alex", "Search", "rechnung", time.Time{}, lim); KindOf(err) != KindLimit {
		t.Errorf("the folder too large: %v", err)
	}
	// The folder searched refused, or a query of nothing: errors.
	if _, _, err := search(t, s, ctx, "sam", "", "x", time.Time{}, DefaultSearchLimits); KindOf(err) != KindAccessDenied {
		t.Errorf("sam's share: %v", err)
	}
	if _, _, err := search(t, s, ctx, "alex", "Search", "", time.Time{}, DefaultSearchLimits); err == nil {
		t.Error("an empty query was searched")
	}
}

// A request that ends stops the search before the next folder: no folder
// is listed after the one during which it ended. Its deadline, instead,
// ends it with what was found.
func TestSearchStops(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx, cancel := context.WithCancel(context.Background())
	listed, after := 0, -1
	searchHook = func(string) {
		listed++
		if listed == 2 {
			cancel()
			after = 0
		} else if after >= 0 {
			after++
		}
	}
	t.Cleanup(func() { searchHook = nil })
	if _, _, err := search(t, s, ctx, "alex", "Search", "rechnung", time.Time{}, DefaultSearchLimits); KindOf(err) != KindCanceled {
		t.Fatalf("a request that ended: %v", err)
	}
	if after > 1 {
		t.Fatalf("%d folders listed after the request ended", after)
	}

	dctx, dcancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer dcancel()
	listed = 0
	searchHook = func(string) {
		listed++
		if listed == 2 {
			<-dctx.Done()
		}
	}
	got, res, err := search(t, s, dctx, "alex", "Search", "rechnung", time.Time{}, DefaultSearchLimits)
	if err != nil || !res.Truncated || res.Reason != "time" || len(got) != 1 {
		t.Fatalf("after the deadline: %q, %+v, %v", got, res, err)
	}
}

// A search in a snapshot finds what was there then.
func TestSearchAt(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	if got, _, err := search(t, s, ctx, "Versions", "", "gone", at1300, DefaultSearchLimits); err != nil || len(got) != 1 || got[0] != "Docs/gone.txt" {
		t.Fatalf("gone.txt at 13:00: %q, %v", got, err)
	}
	if got, _, err := search(t, s, ctx, "Versions", "", "gone", time.Time{}, DefaultSearchLimits); err != nil || len(got) != 0 {
		t.Fatalf("gone.txt now: %q, %v", got, err)
	}
}
