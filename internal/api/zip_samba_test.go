package api

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
)

func TestZipBrowse(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	var z bytes.Buffer
	w := zip.NewWriter(&z)
	for _, f := range []struct{ name, body string }{
		{"Urlaub/", ""},
		{"Urlaub/notizen.txt", "Strand\n"},
		{"../../böse.html", "<script>alert(1)</script>"},
	} {
		fw, _ := w.Create(f.name)
		_, _ = fw.Write([]byte(f.body))
	}
	_ = w.Close()
	name := "archiv-" + time.Now().Format("150405.000") + ".zip"
	if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name), bytes.NewReader(z.Bytes()), ferryReq, c); res.StatusCode != 201 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })

	list := decode[v1.ZipList](t, h.do(t, "GET", "/api/v1/shares/alex/zip?path="+q(name), nil, nil, c))
	if len(list.Entries) != 3 || list.Entries[0].Type != v1.EntryTypeFolder || list.Entries[2].Name != "../../böse.html" || !list.Entries[1].Readable {
		t.Fatalf("list: %+v", list)
	}
	get := func(entry, disp string) *httpResponse {
		res := h.do(t, "GET", "/api/v1/shares/alex/zip/entry?path="+q(name)+"&entry="+q(entry)+disp, nil, nil, c)
		b, _ := io.ReadAll(res.Body)
		return &httpResponse{status: res.StatusCode, body: string(b), disp: res.Header.Get("Content-Disposition"), ctype: res.Header.Get("Content-Type"), csp: res.Header.Get("Content-Security-Policy")}
	}
	if r := get("Urlaub/notizen.txt", "&disposition=inline"); r.status != 200 || r.body != "Strand\n" || !strings.HasPrefix(r.disp, "inline") || !strings.HasPrefix(r.csp, "sandbox") {
		t.Fatalf("an entry inline: %+v", r)
	}
	// A path-like name goes under its base name only; HTML, asked for
	// inline, is plain text in the sandbox, as a file's content is, and as
	// a download never HTML either.
	r := get("../../böse.html", "&disposition=inline")
	if r.status != 200 || !strings.Contains(r.disp, `filename*=UTF-8''b%C3%B6se.html`) || strings.Contains(r.disp, "..") ||
		r.ctype != "text/plain; charset=utf-8" || !strings.HasPrefix(r.csp, "sandbox") {
		t.Fatalf("a hostile name inline: %+v", r)
	}
	r = get("../../böse.html", "")
	if r.status != 200 || !strings.HasPrefix(r.disp, "attachment") || r.ctype != "application/octet-stream" {
		t.Fatalf("a hostile name: %+v", r)
	}
	if r := get("nicht da.txt", ""); r.status != 404 {
		t.Fatalf("a missing entry: %d", r.status)
	}
	if r := get("Urlaub/", ""); r.status != 404 {
		t.Fatalf("a folder entry: %d", r.status)
	}
	// Not a ZIP.
	if res := h.do(t, "GET", "/api/v1/shares/alex/zip?path=hello.txt", nil, nil, c); res.StatusCode != 415 {
		t.Fatalf("hello.txt as an archive: %d", res.StatusCode)
	}
}

type httpResponse struct {
	status                 int
	body, disp, ctype, csp string
}
