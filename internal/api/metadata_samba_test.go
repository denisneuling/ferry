package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	v1 "github.com/metapatrol/ferry/api/v1"
)

// TestMetadata reads what files on the test Samba say about themselves
// through the API: an image's size, a text's lines, a folder refused, the
// ETag honoured — and none of the file's words in the log.
func TestMetadata(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "meta-" + hex.EncodeToString(b)
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c)
	})

	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 640, 480)), nil); err != nil {
		t.Fatal(err)
	}
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/photo.jpg"), bytes.NewReader(photo.Bytes()), ferryReq, c)
	const sentinel = "Leuchtturm-Sentinel-4711"
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/notes.md"), strings.NewReader("# "+sentinel+"\r\nzwei\r\ndrei\r\n"), ferryReq, c)

	res := h.do(t, "GET", "/api/v1/shares/alex/metadata?path="+q(dir+"/photo.jpg"), nil, nil, c)
	m := decode[v1.FileMetadata](t, res)
	if res.StatusCode != 200 || m.Kind != v1.FileMetadataKindImage || *m.Format != "JPEG" || *m.Width != 640 || *m.Height != 480 || m.Camera != nil {
		t.Fatalf("photo: %d %+v", res.StatusCode, m)
	}
	tag := res.Header.Get("ETag")
	if res := h.do(t, "GET", "/api/v1/shares/alex/metadata?path="+q(dir+"/photo.jpg"), nil, map[string]string{"If-None-Match": tag}, c); res.StatusCode != 304 {
		t.Fatalf("revalidation: %d", res.StatusCode)
	}

	m = decode[v1.FileMetadata](t, h.do(t, "GET", "/api/v1/shares/alex/metadata?path="+q(dir+"/notes.md"), nil, nil, c))
	if m.Kind != v1.FileMetadataKindText || *m.Lines != 3 || *m.LineEndings != v1.FileMetadataLineEndingsCRLF || *m.Encoding != v1.FileMetadataEncodingAscii {
		t.Fatalf("text: %+v", m)
	}

	m = decode[v1.FileMetadata](t, h.do(t, "GET", "/api/v1/shares/alex/metadata?path=big.bin", nil, nil, c))
	if m.Kind != v1.FileMetadataKindOther {
		t.Fatalf("random bytes: %+v", m)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/metadata?path="+q(dir), nil, nil, c); res.StatusCode != 409 || problemCode(t, res) != "is_a_folder" {
		t.Fatalf("a folder: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/shares/sam/metadata?path=private.txt", nil, nil, c); res.StatusCode != 403 {
		t.Fatalf("someone else's file: %d", res.StatusCode)
	}
	if strings.Contains(h.log.String(), sentinel) || strings.Contains(h.log.String(), dir) {
		t.Fatal("a file's contents or name reached the log")
	}
}
