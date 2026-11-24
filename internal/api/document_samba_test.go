package api

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
)

// An OpenDocument text: its thumbnail and its text, through the API.
func TestDocuments(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	var thumb bytes.Buffer
	_ = png.Encode(&thumb, image.NewRGBA(image.Rect(0, 0, 60, 80)))
	var z bytes.Buffer
	w := zip.NewWriter(&z)
	for name, body := range map[string][]byte{
		"mimetype":                 []byte("application/vnd.oasis.opendocument.text"),
		"Thumbnails/thumbnail.png": thumb.Bytes(),
		"content.xml": []byte(`<office:document-content xmlns:office="o" xmlns:text="t"><office:body><office:text>` +
			`<text:p>Liebe Familie Beispiel,</text:p><text:p>&lt;b&gt;bitte&lt;/b&gt;</text:p></office:text></office:body></office:document-content>`),
	} {
		fw, _ := w.Create(name)
		_, _ = fw.Write(body)
	}
	_ = w.Close()
	name := "brief-" + time.Now().Format("150405.000") + ".odt"
	if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name), bytes.NewReader(z.Bytes()), ferryReq, c); res.StatusCode != 201 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })

	res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=160&path="+q(name), nil, nil, c)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumbnail: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	txt := decode[v1.DocumentText](t, h.do(t, "GET", "/api/v1/shares/alex/text?path="+q(name), nil, nil, c))
	if txt.Text != "Liebe Familie Beispiel,\n<b>bitte</b>\n" || txt.Truncated {
		t.Fatalf("text: %+v", txt)
	}
	// Not a document: a plain text file.
	if res := h.do(t, "GET", "/api/v1/shares/alex/text?path=hello.txt", nil, nil, c); res.StatusCode != 415 {
		t.Fatalf("a text file's document text: %d", res.StatusCode)
	}
}

// A spreadsheet's first sheet, through the API.
func TestSheets(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	var z bytes.Buffer
	w := zip.NewWriter(&z)
	for name, body := range map[string]string{
		"content.xml": `<office:document-content xmlns:office="o" xmlns:table="t" xmlns:text="x"><office:body><office:spreadsheet>` +
			`<table:table table:name="Haushalt"><table:table-row><table:table-cell><text:p>Miete</text:p></table:table-cell>` +
			`<table:table-cell><text:p>&lt;b&gt;850&lt;/b&gt;</text:p></table:table-cell></table:table-row>` +
			`<table:table-row table:number-rows-repeated="1048575"><table:table-cell table:number-columns-repeated="1024"/></table:table-row>` +
			`</table:table></office:spreadsheet></office:body></office:document-content>`,
	} {
		fw, _ := w.Create(name)
		_, _ = fw.Write([]byte(body))
	}
	_ = w.Close()
	name := "haushalt-" + time.Now().Format("150405.000") + ".ods"
	if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name), bytes.NewReader(z.Bytes()), ferryReq, c); res.StatusCode != 201 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })

	s := decode[v1.Sheet](t, h.do(t, "GET", "/api/v1/shares/alex/sheet?path="+q(name), nil, nil, c))
	if s.Name != "Haushalt" || len(s.Rows) != 1 || len(s.Rows[0]) != 2 || s.Rows[0][1] != "<b>850</b>" || s.Truncated {
		t.Fatalf("sheet: %+v", s)
	}
	if len(s.Names) != 1 || s.Names[0] != "Haushalt" || s.Index != 0 {
		t.Fatalf("names: %+v", s)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/sheet?sheet=1&path="+q(name), nil, nil, c); res.StatusCode != 415 {
		t.Fatalf("a second sheet that is not there: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/sheet?path=hello.txt", nil, nil, c); res.StatusCode != 415 {
		t.Fatalf("a text file's sheet: %d", res.StatusCode)
	}
}
