package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image/jpeg"
	"os"
	"strings"
	"testing"

	"github.com/metapatrol/ferry/internal/preview"
	"github.com/metapatrol/ferry/internal/smb"
)

// onePagePDF is a page of text — or, with no lines, an empty page.
func onePagePDF(lines int) []byte {
	var c strings.Builder
	for i := range lines {
		fmt.Fprintf(&c, "BT /F1 12 Tf 72 %d Td (Line %d of a letter) Tj ET\n", 760-i*16, i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", c.Len()+1, c.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	var offs []int
	for i, o := range objs {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// A PDF's thumbnail is its first page, read from the share as pdfium asks
// for it; a page it renders white is an icon. The budget is free again
// afterwards.
func TestPDFThumbnail(t *testing.T) {
	addr := os.Getenv("FERRY_TEST_SMB_SERVER")
	if addr == "" {
		t.Skip("FERRY_TEST_SMB_SERVER is not set (make dev-samba)")
	}
	cfg := testConfig()
	cfg.SMBServer = addr
	cfg.MaxUpload = 8 << 20
	cfg.SMBConnections = 3
	cfg.PreviewPDF = true
	cfg.PreviewConcurrency = 1
	// The compiled code, as the image ships it: compiling inside a request
	// takes longer than the request may, under -race above all.
	cfg.WASMDir = t.TempDir()
	if err := preview.Precompile(cfg.WASMDir); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, cfg, smb.LoginGroup)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "pdf-" + hex.EncodeToString(b)
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c)
	})
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/letter.pdf"), bytes.NewReader(onePagePDF(20)), ferryReq, c)
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/empty.pdf"), bytes.NewReader(onePagePDF(0)), ferryReq, c)
	// pdfium takes a header after other bytes; so does ferry's sniffing.
	late := append([]byte(strings.Repeat("x", 500)+"\n"), onePagePDF(20)...)
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/late.pdf"), bytes.NewReader(late), ferryReq, c)

	info := decode[map[string]any](t, h.do(t, "GET", "/api/v1/info", nil, nil, nil))
	if p, _ := info["previews"].([]any); len(p) != 1 || p[0] != "pdf" {
		t.Fatalf("info's previews: %v", info["previews"])
	}
	res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=320&path="+q(dir+"/letter.pdf"), nil, nil, c)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("a letter's thumbnail: %d", res.StatusCode)
	}
	img, err := jpeg.Decode(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dy() != 320 || b.Dx() < 224 || b.Dx() > 228 {
		t.Fatalf("an A4 page in 320: %v", b)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=160&path="+q(dir+"/late.pdf"), nil, nil, c); res.StatusCode != 200 {
		t.Fatalf("a PDF whose header comes late: %d", res.StatusCode)
	}
	res = h.do(t, "GET", "/api/v1/shares/alex/thumbnail?path="+q(dir+"/empty.pdf"), nil, nil, c)
	if res.StatusCode != 415 || problemCode(t, res) != "not_an_image" {
		t.Fatalf("an empty page: %d", res.StatusCode)
	}
	if used, _ := h.api.ThumbnailMemory(); used != 0 {
		t.Fatalf("%d bytes still taken from the budget", used)
	}
}

// With a thumbnail budget below one page, PDF previews are off from the
// start: Info does not list them, so the interface does not ask.
func TestPreviewsOffBelowBudget(t *testing.T) {
	cfg := testConfig()
	cfg.PreviewPDF = true
	cfg.ThumbnailMemory = 128 << 20
	h := newHarness(t, cfg, nil)
	info := decode[map[string]any](t, h.do(t, "GET", "/api/v1/info", nil, nil, nil))
	if _, ok := info["previews"]; ok {
		t.Fatalf("previews listed with a budget below a page: %v", info["previews"])
	}
}
