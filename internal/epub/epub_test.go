package epub

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func zipOf(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		fw, _ := w.Create(name)
		_, _ = fw.Write([]byte(body))
	}
	_ = w.Close()
	return b.Bytes()
}

const container = `<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`

const opf = `<package><metadata><dc:title xmlns:dc="dc">Ein Sommer am Meer</dc:title></metadata><manifest>
<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
<item id="c1" href="text/kapitel%201.xhtml" media-type="application/xhtml+xml"/>
<item id="c2" href="text/comic.xhtml" media-type="application/xhtml+xml"/>
<item id="out" href="../../outside.xhtml" media-type="application/xhtml+xml"/>
<item id="pic" href="images/strand.jpg" media-type="text/html"/>
</manifest><spine><itemref idref="c1"/><itemref idref="out"/><itemref idref="c2"/></spine></package>`

const nav = `<html xmlns="http://www.w3.org/1999/xhtml"><body><nav><ol>
<li><a href="text/kapitel%201.xhtml">Erstes &lt;b&gt;Kapitel&lt;/b&gt;</a></li><li><a href="text/comic.xhtml#p1">Bilder</a></li></ol></nav></body></html>`

const chapter = `<?xml version="1.0"?>
<!DOCTYPE html [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;&lol;">]>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>x</title><style>p{color:red}</style><script>alert(1)</script></head>
<body onload="alert(1)"><section><h1 class="t" onclick="alert(1)">Am Strand</h1>
<p>Es war <i>warm</i>&#160;&mdash; <b>sehr</b> warm. <a href="javascript:alert(1)">Ein Link</a> &lol2;</p>
<p><img src="../images/strand.jpg" alt="Der Strand"/><img src="../images/fehlt.jpg" alt="Ein Bild, das fehlt"/><img src="../../../etc/passwd" alt=""/><img src="data:image/png;base64,AAAA" alt="data"/></p>
<iframe src="https://example.org"></iframe><form><input value="x"/></form>
<svg><script>alert(1)</script><image href="../images/strand.jpg"/></svg>
<ul><li>eins</li><li>zwei</li></ul></section></body></html>`

const comic = `<html xmlns="http://www.w3.org/1999/xhtml"><body><div><img src="../images/strand.jpg"/></div></body></html>`

func book(t testing.TB, extra map[string]string) []byte {
	files := map[string]string{
		"META-INF/container.xml":     container,
		"OEBPS/content.opf":          opf,
		"OEBPS/nav.xhtml":            nav,
		"OEBPS/text/kapitel 1.xhtml": chapter,
		"OEBPS/text/comic.xhtml":     comic,
		"OEBPS/images/strand.jpg":    "jpeg",
		"outside.xhtml":              "<html><body><p>draussen</p></body></html>",
	}
	for k, v := range extra {
		files[k] = v
	}
	return zipOf(t, files)
}

func TestOpen(t *testing.T) {
	z := book(t, nil)
	b, _, err := Open(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Ein Sommer am Meer" || len(b.Chapters) != 2 {
		t.Fatalf("%+v", b)
	}
	if b.Chapters[0].Entry != "OEBPS/text/kapitel 1.xhtml" || b.Chapters[0].Title != "Erstes <b>Kapitel</b>" || b.Chapters[1].Title != "Bilder" {
		t.Fatalf("chapters %+v", b.Chapters)
	}
}

func TestChapter(t *testing.T) {
	z := book(t, nil)
	b, es, _ := Open(bytes.NewReader(z), int64(len(z)))
	nodes, more, err := ReadChapter(bytes.NewReader(z), int64(len(z)), es, b, b.Chapters[0].Entry)
	if err != nil || more {
		t.Fatal(err, more)
	}
	j, _ := json.Marshal(nodes)
	out := string(j)
	for _, bad := range []string{"alert", "javascript", "onclick", "onload", "iframe", "example.org", "color:red", "passwd", "data:", "value"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q in %s", bad, out)
		}
	}
	for _, good := range []string{`"tag":"h1"`, `"tag":"em","children":[{"text":"warm"}]`, `"tag":"strong"`, "Ein Link", `"src":"OEBPS/images/strand.jpg","alt":"Der Strand"`, `"text":"Ein Bild, das fehlt"`, `"tag":"li"`, " —"} {
		if !strings.Contains(out, good) {
			t.Errorf("no %s in %s", good, out)
		}
	}
	walk(t, nodes)
	// A comic's page: a picture alone.
	nodes, _, err = ReadChapter(bytes.NewReader(z), int64(len(z)), es, b, b.Chapters[1].Entry)
	if j, _ := json.Marshal(nodes); err != nil || !strings.Contains(string(j), `"src":"OEBPS/images/strand.jpg"`) {
		t.Fatalf("comic: %s %v", j, err)
	}
	// Only the book's own chapters are read.
	if _, _, err := ReadChapter(bytes.NewReader(z), int64(len(z)), es, b, "outside.xhtml"); !errors.Is(err, ErrNotBook) {
		t.Fatalf("a part that is no chapter: %v", err)
	}
}

func TestEncrypted(t *testing.T) {
	drm := `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/></EncryptedData></encryption>`
	z := book(t, map[string]string{"META-INF/encryption.xml": drm})
	if _, _, err := Open(bytes.NewReader(z), int64(len(z))); !errors.Is(err, ErrEncrypted) {
		t.Fatalf("DRM: %v", err)
	}
	fonts := `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/></EncryptedData></encryption>`
	z = book(t, map[string]string{"META-INF/encryption.xml": fonts})
	if _, _, err := Open(bytes.NewReader(z), int64(len(z))); err != nil {
		t.Fatalf("obfuscated fonts only: %v", err)
	}
}

// walk checks a tree: only known elements, and pictures only as archive names.
func walk(t testing.TB, ns []Node) {
	for _, n := range ns {
		if n.Tag != "" && n.Tag != "div" && !Tags[n.Tag] {
			t.Fatalf("tag %q", n.Tag)
		}
		if n.Tag == "img" && (n.Src == "" || strings.Contains(n.Src, ":") || strings.HasPrefix(n.Src, "/") || strings.HasPrefix(n.Src, "..")) {
			t.Fatalf("img src %q", n.Src)
		}
		if n.Tag != "img" && n.Src != "" {
			t.Fatalf("src on %q", n.Tag)
		}
		walk(t, n.Children)
	}
}

func FuzzChapter(f *testing.F) {
	f.Add(chapter)
	f.Add(comic)
	f.Add(`<!DOCTYPE x [<!ENTITY a "&a;&a;">]><html><body><p>&a;</p></body></html>`)
	f.Add(strings.Repeat("<div>", 200) + "<body>" + strings.Repeat("<p><b>", 100) + "x")
	names := map[string]bool{"OEBPS/images/strand.jpg": true, "x.png": true}
	f.Fuzz(func(t *testing.T, s string) {
		nodes, _, err := parse(strings.NewReader(s), "OEBPS/text", names)
		if err != nil {
			return
		}
		walk(t, nodes)
		depth := 0
		var d func([]Node, int)
		d = func(ns []Node, k int) {
			depth = max(depth, k)
			for _, n := range ns {
				d(n.Children, k+1)
			}
		}
		d(nodes, 0)
		if depth > maxDepth+2 {
			t.Fatalf("depth %d", depth)
		}
	})
}
