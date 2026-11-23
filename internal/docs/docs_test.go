package docs

import (
	"archive/zip"
	"bytes"
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
