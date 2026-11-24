package docs

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/denisneuling/ferry/internal/zipread"
)

func zipOf(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const docx = `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>Sehr geehrte </w:t></w:r><w:r><w:t>Familie Beispiel,</w:t></w:r></w:p>
<w:p><w:r><w:t>Betrag:</w:t><w:tab/><w:t>42 €</w:t></w:r></w:p>
<w:p><w:r><w:t>&lt;script&gt;alert(1)&lt;/script&gt;</w:t></w:r></w:p></w:body></w:document>`

const odt = `<?xml version="1.0"?><office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"><office:automatic-styles><style>not text</style></office:automatic-styles><office:body><office:text>
<text:h>Einkaufsliste</text:h><text:p>Milch<text:s/>und<text:tab/>Brot</text:p></office:text></office:body></office:document-content>`

func TestText(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"docx", map[string]string{"word/document.xml": docx}, "Sehr geehrte Familie Beispiel,\nBetrag:\t42 €\n<script>alert(1)</script>\n"},
		{"odt", map[string]string{"content.xml": odt}, "Einkaufsliste\nMilch und\tBrot\n"},
	} {
		z := zipOf(t, c.files)
		got, more, err := Text(bytes.NewReader(z), int64(len(z)), 1<<20)
		if err != nil || more || got != c.want {
			t.Errorf("%s: %q, %v, %v", c.name, got, more, err)
		}
	}
	z := zipOf(t, map[string]string{"word/document.xml": docx})
	if got, more, _ := Text(bytes.NewReader(z), int64(len(z)), 10); !more || len(got) > 11 {
		t.Errorf("bounded: %q, %v", got, more)
	}
	z = zipOf(t, map[string]string{"xl/workbook.xml": "<x/>"})
	if _, _, err := Text(bytes.NewReader(z), int64(len(z)), 100); err != ErrNone {
		t.Errorf("a spreadsheet's text: %v", err)
	}
}

func TestThumbnail(t *testing.T) {
	cases := map[string]map[string]string{
		"Thumbnails/thumbnail.png": {"mimetype": "application/vnd.oasis.opendocument.text", "Thumbnails/thumbnail.png": "png", "content.xml": odt},
		"docProps/thumbnail.jpeg":  {"word/document.xml": docx, "docProps/thumbnail.jpeg": "jpeg"},
		"OEBPS/images/cover.jpg": {
			"META-INF/container.xml": `<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`,
			"OEBPS/content.opf": `<package><metadata><meta name="cover" content="c"/></metadata><manifest>
				<item id="x" href="text.xhtml" media-type="application/xhtml+xml"/>
				<item id="c" href="images/cover.jpg" media-type="image/jpeg"/></manifest></package>`,
			"OEBPS/images/cover.jpg": "jpeg",
		},
		"cover.png": {
			"META-INF/container.xml": `<container><rootfiles><rootfile full-path="book.opf"/></rootfiles></container>`,
			"book.opf":               `<package><manifest><item id="i" href="cover.png" media-type="image/png" properties="cover-image"/></manifest></package>`,
			"cover.png":              "png",
		},
	}
	for want, files := range cases {
		z := zipOf(t, files)
		e, err := Thumbnail(bytes.NewReader(z), int64(len(z)))
		if err != nil || e.Name != want {
			t.Errorf("%s: %q, %v", want, e.Name, err)
		}
	}
	// A cover that points out of the archive's names is not found.
	z := zipOf(t, map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="book.opf"/></rootfiles></container>`,
		"book.opf":               `<package><manifest><item id="i" href="../../etc/passwd" media-type="image/png" properties="cover-image"/></manifest></package>`,
	})
	if _, err := Thumbnail(bytes.NewReader(z), int64(len(z))); err != ErrNone {
		t.Errorf("a cover outside: %v", err)
	}
}

// FuzzText feeds the readers arbitrary archives: no panic, the text held
// to its bound.
func FuzzText(f *testing.F) {
	f.Add(zipOf(f, map[string]string{"word/document.xml": docx}))
	f.Add(zipOf(f, map[string]string{"content.xml": odt, "Thumbnails/thumbnail.png": "x"}))
	f.Fuzz(func(t *testing.T, b []byte) {
		got, _, _ := Text(bytes.NewReader(b), int64(len(b)), 4096)
		if len(got) > 4097 {
			t.Fatalf("%d bytes of text", len(got))
		}
		if e, err := Thumbnail(bytes.NewReader(b), int64(len(b))); err == nil && !e.Readable() {
			t.Fatal("an unreadable thumbnail")
		}
		_ = zipread.ErrNotZip
	})
}

func TestSlideText(t *testing.T) {
	slide := func(lines ...string) string {
		var b strings.Builder
		b.WriteString(`<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree>`)
		for _, l := range lines {
			b.WriteString(`<p:sp><p:txBody><a:p><a:r><a:t>` + l + `</a:t></a:r></a:p></p:txBody></p:sp>`)
		}
		b.WriteString(`</p:spTree></p:cSld></p:sld>`)
		return b.String()
	}
	z := zipOf(t, map[string]string{
		"ppt/presentation.xml":             "<p/>",
		"ppt/slides/slide10.xml":           slide("Zehn"),
		"ppt/slides/slide2.xml":            slide("Zwei", "&lt;b&gt;fett&lt;/b&gt;"),
		"ppt/slides/slide1.xml":            slide("Urlaub 2025"),
		"ppt/slides/_rels/slide1.xml.rels": "<x/>",
	})
	got, more, err := Text(bytes.NewReader(z), int64(len(z)), 1<<20)
	if err != nil || more || got != "Urlaub 2025\n\nZwei\n<b>fett</b>\n\nZehn\n" {
		t.Fatalf("%q, %v, %v", got, more, err)
	}
	// Slides moved: presentation.xml's order counts, not the files'
	// numbers; a hidden slide is left out.
	z = zipOf(t, map[string]string{
		"ppt/presentation.xml": `<p:presentation xmlns:p="p" xmlns:r="r"><p:sldIdLst>
<p:sldId id="258" r:id="rId9"/><p:sldId id="256" r:id="rId7"/><p:sldId id="257" r:id="rId8"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId7" Target="slides/slide1.xml"/>
<Relationship Id="rId8" Target="slides/slide2.xml"/><Relationship Id="rId9" Target="/ppt/slides/slide3.xml"/></Relationships>`,
		"ppt/slides/slide1.xml": slide("Eins"),
		"ppt/slides/slide2.xml": strings.Replace(slide("versteckt"), "<p:sld ", `<p:sld show="0" `, 1),
		"ppt/slides/slide3.xml": slide("Drei, jetzt vorn"),
	})
	got, _, err = Text(bytes.NewReader(z), int64(len(z)), 1<<20)
	if err != nil || got != "Drei, jetzt vorn\n\nEins\n" {
		t.Fatalf("moved: %q, %v", got, err)
	}
	odp := `<office:document-content><office:body><office:presentation>
<draw:page><draw:frame><draw:text-box><text:p>Titel</text:p></draw:text-box></draw:frame><presentation:notes><text:p>geheime Notiz</text:p></presentation:notes></draw:page>
<draw:page><draw:frame><draw:text-box><text:p>Folie zwei</text:p></draw:text-box></draw:frame></draw:page>
</office:presentation></office:body></office:document-content>`
	z = zipOf(t, map[string]string{"content.xml": odp})
	got, _, err = Text(bytes.NewReader(z), int64(len(z)), 1<<20)
	if err != nil || got != "Titel\n\nFolie zwei\n" {
		t.Fatalf("odp: %q, %v", got, err)
	}
}
