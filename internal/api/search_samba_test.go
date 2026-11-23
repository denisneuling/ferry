package api

import (
	"bufio"
	"encoding/json"
	"testing"

	v1 "github.com/denisneuling/ferry/api/v1"
)

func TestSearchAPI(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	read := func(url string) (int, []string, *v1.SearchEnd) {
		t.Helper()
		res := h.do(t, "GET", url, nil, nil, c)
		if res.StatusCode != 200 {
			return res.StatusCode, nil, nil
		}
		if ct := res.Header.Get("Content-Type"); ct != ndjson {
			t.Fatalf("Content-Type %q", ct)
		}
		var hits []string
		var end *v1.SearchEnd
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			var line map[string]json.RawMessage
			if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
				t.Fatalf("line %q: %v", sc.Text(), err)
			}
			switch {
			case line["hit"] != nil:
				var e v1.Entry
				_ = json.Unmarshal(line["hit"], &e)
				hits = append(hits, e.Path)
			case line["end"] != nil:
				end = &v1.SearchEnd{}
				_ = json.Unmarshal(sc.Bytes(), end)
			default:
				t.Fatalf("a line of its own: %s", sc.Text())
			}
		}
		return 200, hits, end
	}

	status, hits, end := read("/api/v1/shares/alex/search?path=Search&q=" + q("Rechnung"))
	if status != 200 || len(hits) != 4 || end == nil || end.End.Hits != 4 || end.End.Skipped != 1 || end.End.Truncated {
		t.Fatalf("search: %d %q %+v", status, hits, end)
	}
	// In a snapshot: what was there then.
	if status, hits, _ := read("/api/v1/shares/Versions/search?q=gone&at=" + q("@GMT-2026.11.23-13.00.09")); status != 200 || len(hits) != 1 || hits[0] != "Docs/gone.txt" {
		t.Fatalf("search at 13:00: %d %q", status, hits)
	}
	// Refused before the first hit: ordinary problem responses.
	for url, want := range map[string]int{
		"/api/v1/shares/alex/search?path=Search":                      400,
		"/api/v1/shares/alex/search?path=Search&q=" + q("  "):         400,
		"/api/v1/shares/sam/search?q=x":                               403,
		"/api/v1/shares/alex/search?path=nowhere&q=x":                 404,
		"/api/v1/shares/alex/search?path=Search&q=x&at=" + q("later"): 400,
	} {
		if status, _, _ := read(url); status != want {
			t.Errorf("%s: %d, want %d", url, status, want)
		}
	}
}
