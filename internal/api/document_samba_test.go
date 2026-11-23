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
