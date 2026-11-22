package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"testing"
	"time"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/config"
	"github.com/metapatrol/ferry/internal/smb"
)

// manyEntries is the size of hack/samba's alex/Many.
const manyEntries = 60000

func sambaHarnessWith(t testing.TB, change func(*config.Config)) *harness {
	t.Helper()
	addr := os.Getenv("FERRY_TEST_SMB_SERVER")
	if addr == "" {
		t.Skip("FERRY_TEST_SMB_SERVER is not set (make dev-samba)")
	}
	cfg := testConfig()
	cfg.SMBServer = addr
	cfg.SMBConnections = 3
	change(&cfg)
	return newHarness(t, cfg, smb.LoginGroup)
}

// listRaw asks for a listing without reading it in advance, unlike h.do:
// these tests read it as a browser does, line by line.
func (h *harness) listRaw(t testing.TB, path, accept string, c *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", h.srv.URL+"/api/v1/shares/alex/entries?path="+q(path), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", accept)
	req.AddCookie(c)
	res, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// streamLine is any line of an NDJSON listing.
type streamLine struct {
	v1.Entry
	End *struct {
		Count int `json:"count"`
	} `json:"end"`
	Problem *v1.Problem `json:"problem"`
}

// readStream reads an NDJSON listing to its last line.
// first is the time from start to the first entry.
func readStream(t testing.TB, res *http.Response, start time.Time) (names map[string]bool, last streamLine, first time.Duration) {
	t.Helper()
	names = map[string]bool{}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var l streamLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		if l.End != nil || l.Problem != nil {
			last = l
			if sc.Scan() {
				t.Fatalf("a line after the last: %q", sc.Text())
			}
			return names, last, first
		}
		if first == 0 {
			first = time.Since(start)
		}
		if names[l.Name] {
			t.Fatalf("%s twice", l.Name)
		}
		names[l.Name] = true
	}
	t.Fatalf("the stream ended without a last line after %d entries (%v)", len(names), sc.Err())
	return
}

// TestListLargeFolder lists a folder of 60000 entries as a stream and as one
// document. The stream's first entry is there long before its last, and
// ferry's memory does not grow with the folder.
func TestListLargeFolder(t *testing.T) {
	h := sambaHarnessWith(t, func(c *config.Config) { c.MaxListing = 200000 })
	c := h.login(t, "alex", alexPassword)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	res := h.listRaw(t, "Many", ndjson, c)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != ndjson {
		t.Fatalf("status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	names, last, first := readStream(t, res, start)
	all := time.Since(start)
	runtime.ReadMemStats(&after)
	if last.End == nil || last.End.Count != manyEntries || len(names) != manyEntries {
		t.Fatalf("%d entries, last line %+v", len(names), last)
	}
	// The client decodes in this process too, so the allocations are both
	// sides'; the heap in use afterwards is what stays.
	t.Logf("stream: first entry after %v, all %d after %v; heap in use %+.1f MiB",
		first, manyEntries, all, (float64(after.HeapInuse)-float64(before.HeapInuse))/(1<<20))
	if first > all/4 {
		t.Errorf("the first entry came after %v of %v: the listing is not streamed", first, all)
	}

	start = time.Now()
	list := decode[v1.EntryList](t, h.do(t, "GET", "/api/v1/shares/alex/entries?path=Many", nil, nil, c))
	if len(list.Entries) != manyEntries || list.Path != "Many" {
		t.Fatalf("json: %d entries, path %q", len(list.Entries), list.Path)
	}
	t.Logf("json: %d entries after %v", manyEntries, time.Since(start))
}

// TestListBeyondTheLimit: a folder larger than FERRY_MAX_LISTING is
// too_large with the limit — as a problem response when the first batch
// already exceeds it, as the stream's last line when a later one does, and
// as a cut-off connection for a JSON listing that had begun.
func TestListBeyondTheLimit(t *testing.T) {
	h := sambaHarnessWith(t, func(c *config.Config) { c.MaxListing = 100 })
	c := h.login(t, "alex", alexPassword)
	res := h.do(t, "GET", "/api/v1/shares/alex/entries?path=Many", nil, map[string]string{"Accept": ndjson}, c)
	p := decode[v1.Problem](t, res)
	if res.StatusCode != 413 || p.Code != "too_large" || p.Limit == nil || *p.Limit != 100 {
		t.Fatalf("status %d, problem %+v", res.StatusCode, p)
	}

	h = sambaHarnessWith(t, func(c *config.Config) { c.MaxListing = 2000 })
	c = h.login(t, "alex", alexPassword)
	names, last, _ := readStream(t, h.listRaw(t, "Many", ndjson, c), time.Now())
	if last.Problem == nil || last.Problem.Code != "too_large" || last.Problem.Limit == nil || *last.Problem.Limit != 2000 {
		t.Fatalf("last line %+v after %d entries", last, len(names))
	}
	if len(names) > 2000 {
		t.Fatalf("%d entries beyond a limit of 2000", len(names))
	}

	res = h.listRaw(t, "Many", "application/json", c)
	if res.StatusCode != 200 {
		t.Fatalf("json status %d", res.StatusCode)
	}
	var list v1.EntryList
	err := json.NewDecoder(res.Body).Decode(&list)
	if err == nil {
		t.Fatalf("a JSON listing cut off by the limit parsed as %d entries", len(list.Entries))
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Logf("cut off as %v", err)
	}

	// The session goes on after all three.
	small := decode[v1.EntryList](t, h.do(t, "GET", "/api/v1/shares/alex/entries?path=", nil, nil, c))
	if len(small.Entries) == 0 {
		t.Fatal("the session after the limit lists nothing")
	}
}

// TestDeleteHasItsOwnLimit: the recursive delete's bound is FERRY_MAX_DELETE,
// not the listing's.
func TestDeleteHasItsOwnLimit(t *testing.T) {
	h := sambaHarnessWith(t, func(c *config.Config) { c.MaxListing = 200000; c.MaxDelete = 10 })
	c := h.login(t, "alex", alexPassword)
	res := h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path=Many", nil, ferryReq, c)
	p := decode[v1.Problem](t, res)
	if res.StatusCode != 413 || p.Limit == nil || *p.Limit != 10 {
		t.Fatalf("status %d, problem %+v", res.StatusCode, p)
	}
	list := decode[v1.EntryList](t, h.do(t, "GET", "/api/v1/shares/alex/entries?path=Many", nil, nil, c))
	if len(list.Entries) != manyEntries {
		t.Fatalf("the refused delete removed something: %d entries left", len(list.Entries))
	}
}
