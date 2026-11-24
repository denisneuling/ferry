package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
)

func TestBook(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	var z bytes.Buffer
	w := zip.NewWriter(&z)
	for name, body := range map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="b.opf"/></rootfiles></container>`,
		"b.opf": `<package><metadata><title>Buch</title></metadata><manifest><item id="c" href="c.xhtml"/></manifest>
<spine><itemref idref="c"/></spine></package>`,
		"c.xhtml": `<html><body><p onclick="x()">Es war <b>einmal</b><script>alert(1)</script></p><img src="p.png" alt="Bild"/></body></html>`,
		"p.png":   "png",
	} {
		fw, _ := w.Create(name)
		_, _ = fw.Write([]byte(body))
	}
	_ = w.Close()
	name := "buch-" + time.Now().Format("150405.000") + ".epub"
	if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name), bytes.NewReader(z.Bytes()), ferryReq, c); res.StatusCode != 201 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })
	b := decode[v1.Book](t, h.do(t, "GET", "/api/v1/shares/alex/book?path="+q(name), nil, nil, c))
	if b.Title != "Buch" || len(b.Chapters) != 1 || b.Chapters[0].Entry != "c.xhtml" {
		t.Fatalf("book %+v", b)
	}
	res := h.do(t, "GET", "/api/v1/shares/alex/book/chapter?entry=c.xhtml&path="+q(name), nil, nil, c)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("chapter: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	var ch v1.BookChapter
	if err := json.Unmarshal(body, &ch); err != nil || ch.Truncated || len(ch.Nodes) == 0 {
		t.Fatalf("chapter %s, %v", body, err)
	}
	if s := string(body); strings.Contains(s, "alert") || strings.Contains(s, "onclick") || !strings.Contains(s, `"src":"p.png"`) {
		t.Fatalf("chapter %s", s)
	}
	for url, want := range map[string]int{
		"/api/v1/shares/alex/book/chapter?entry=b.opf&path=" + q(name): 415,
		"/api/v1/shares/alex/book?path=hello.txt":                      415,
	} {
		if res := h.do(t, "GET", url, nil, nil, c); res.StatusCode != want {
			t.Errorf("%s: %d, want %d", url, res.StatusCode, want)
		}
	}
}
