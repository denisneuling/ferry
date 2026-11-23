package api

import (
	"io"
	"strings"
	"testing"
	"time"
)

// An editor's save: replaced only over the version it opened.
func TestSaveIfMatch(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	name := "edit-" + time.Now().Format("150405.000") + ".txt"
	path := "/api/v1/shares/alex/content?path=" + q(name)
	if res := h.do(t, "PUT", path, strings.NewReader("one"), ferryReq, c); res.StatusCode != 201 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })
	read := func() (string, string) {
		res := h.do(t, "GET", path, nil, nil, c)
		b, _ := io.ReadAll(res.Body)
		return string(b), res.Header.Get("ETag")
	}
	put := func(body, ifMatch string) int {
		hdr := map[string]string{"X-Ferry-Request": "1", "If-Match": ifMatch}
		return h.do(t, "PUT", path, strings.NewReader(body), hdr, c).StatusCode
	}
	_, opened := read()
	// Saved over the version opened.
	if got := put("two", opened); got != 201 {
		t.Fatalf("save: %d", got)
	}
	now, tag := read()
	if now != "two" || tag == opened {
		t.Fatalf("after the save: %q, %s", now, tag)
	}
	// Someone else's save in between: the stale one is refused, nothing changes.
	if got := put("three", opened); got != 412 {
		t.Fatalf("a stale save: %d", got)
	}
	if now, _ := read(); now != "two" {
		t.Fatalf("a refused save changed the file: %q", now)
	}
	// A file gone since is no file to save over.
	if got := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name+".gone"), strings.NewReader("x"), map[string]string{"X-Ferry-Request": "1", "If-Match": tag}, c).StatusCode; got != 412 {
		t.Fatalf("a save over nothing: %d", got)
	}
}
