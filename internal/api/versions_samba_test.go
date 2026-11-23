package api

import (
	"io"
	"net/http"
	"strings"
	"testing"

	v1 "github.com/denisneuling/ferry/api/v1"
)

// Previous versions through the API, against the Versions share of
// hack/samba (snapshots as a ZFS NAS offers them).
func TestVersionsAPI(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	const base = "/api/v1/shares/Versions/"
	const at1300, at1400 = "@GMT-2026.11.23-13.00.09", "@GMT-2026.11.23-14.00.09"
	body := func(res *http.Response) string {
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}

	res := h.do(t, "GET", base+"versions?path="+q("Docs/letter.txt"), nil, nil, c)
	vl := decode[v1.VersionList](t, res)
	if res.StatusCode != 200 || vl.Truncated || len(vl.Versions) != 2 || vl.Versions[0].At != at1400 || vl.Versions[1].At != at1300 ||
		vl.Versions[1].Size != 3 || vl.Versions[1].Type != v1.EntryTypeFile || vl.Versions[1].Version == "" {
		t.Fatalf("versions of letter.txt: %d %+v", res.StatusCode, vl)
	}
	// A path deleted since has its version; a share without snapshots none.
	if vl := decode[v1.VersionList](t, h.do(t, "GET", base+"versions?path="+q("Docs/gone.txt"), nil, nil, c)); len(vl.Versions) != 1 {
		t.Fatalf("versions of gone.txt: %+v", vl)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/versions?path=hello.txt", nil, nil, c); res.StatusCode != 200 || len(decode[v1.VersionList](t, res).Versions) != 0 {
		t.Fatalf("a share without snapshots: %d", res.StatusCode)
	}

	// The file as it was, with an ETag of its own.
	now := h.do(t, "GET", base+"content?path="+q("Docs/letter.txt"), nil, nil, c)
	old := h.do(t, "GET", base+"content?path="+q("Docs/letter.txt")+"&at="+q(at1300), nil, nil, c)
	if got := body(old); old.StatusCode != 200 || got != "v1\n" {
		t.Fatalf("letter.txt at 13:00: %d %q", old.StatusCode, got)
	}
	if body(now) != "v3 now\n" || old.Header.Get("ETag") == now.Header.Get("ETag") || !strings.Contains(old.Header.Get("ETag"), at1300) {
		t.Fatalf("ETags now %s, then %s", now.Header.Get("ETag"), old.Header.Get("ETag"))
	}
	// A folder as it was, a deleted file in it.
	res = h.do(t, "GET", base+"entries?path=Docs&at="+q(at1300), nil, nil, c)
	if got := body(res); res.StatusCode != 200 || !strings.Contains(got, `"gone.txt"`) || strings.Contains(got, `"new.txt"`) {
		t.Fatalf("Docs at 13:00: %d %s", res.StatusCode, got)
	}
	// The details of a version are told apart from the file's now.
	mNow := h.do(t, "GET", base+"metadata?path="+q("Docs/letter.txt"), nil, nil, c)
	mOld := h.do(t, "GET", base+"metadata?path="+q("Docs/letter.txt")+"&at="+q(at1300), nil, nil, c)
	if mOld.StatusCode != 200 || mOld.Header.Get("ETag") == mNow.Header.Get("ETag") {
		t.Fatalf("metadata ETags now %s, then %s", mNow.Header.Get("ETag"), mOld.Header.Get("ETag"))
	}

	// A time that is no snapshot of the path, or not in the form: 404, 400.
	for _, c2 := range []struct {
		query string
		want  int
	}{
		{"path=" + q("Docs/new.txt") + "&at=" + q(at1300), 404},
		{"path=" + q("Docs/letter.txt") + "&at=" + q("@GMT-2026.11.23-13.00.10"), 404},
		{"path=" + q("Docs/letter.txt") + "&at=" + q("@GMT-2026.11.23-13.00.9"), 400},
		{"path=" + q("Docs/letter.txt") + "&at=" + q("2026-11-23T13:00:09Z"), 400},
		{"path=" + q(at1300+"/Docs/letter.txt"), 400},
	} {
		if res := h.do(t, "GET", base+"content?"+c2.query, nil, nil, c); res.StatusCode != c2.want {
			t.Errorf("content?%s: %d, want %d", c2.query, res.StatusCode, c2.want)
		}
	}

	// Nothing changes with at: 400 version_read_only, and the file is as it was.
	for _, w := range []struct{ method, path string }{
		{"PUT", "content?path=" + q("Docs/letter.txt") + "&overwrite=true"},
		{"DELETE", "entries?path=" + q("Docs/letter.txt")},
		{"POST", "folders?path=" + q("Docs/x")},
		{"POST", "move?"},
	} {
		res := h.do(t, w.method, base+w.path+"&at="+q(at1300), strings.NewReader("{}"), ferryReq, c)
		if res.StatusCode != 400 || decode[v1.Problem](t, res).Code != "version_read_only" {
			t.Errorf("%s %s with at: %d", w.method, w.path, res.StatusCode)
		}
	}
	if got := body(h.do(t, "GET", base+"content?path="+q("Docs/letter.txt"), nil, nil, c)); got != "v3 now\n" {
		t.Fatalf("letter.txt changed: %q", got)
	}

	// Samba decides: sam may not open the share, now or then.
	sc := h.login(t, "sam", "sam-test-password")
	if res := h.do(t, "GET", base+"versions?path="+q("Docs/letter.txt"), nil, nil, sc); res.StatusCode != 403 {
		t.Fatalf("sam's versions: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", base+"content?path="+q("Docs/letter.txt")+"&at="+q(at1300), nil, nil, sc); res.StatusCode != 403 {
		t.Fatalf("sam's content at a time: %d", res.StatusCode)
	}
}
