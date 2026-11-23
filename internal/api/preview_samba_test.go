package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denisneuling/ferry/internal/preview"
	"github.com/denisneuling/ferry/internal/smb"
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

// A preview job past FERRY_PREVIEW_TIMEOUT is a 503 that says so: not the
// 415 of a file that makes no preview, and not cached, so that the next
// request tries again.
func TestPreviewTimeout(t *testing.T) {
	addr := os.Getenv("FERRY_TEST_SMB_SERVER")
	if addr == "" {
		t.Skip("FERRY_TEST_SMB_SERVER is not set (make dev-samba)")
	}
	cfg := testConfig()
	cfg.SMBServer = addr
	cfg.PreviewPDF = true
	cfg.PreviewTimeout = time.Nanosecond // every job runs out
	cfg.CacheDir = t.TempDir()
	cfg.CacheKey = strings.Repeat("k", 32)
	cfg.WASMDir = t.TempDir()
	if err := preview.Precompile(cfg.WASMDir); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, cfg, smb.LoginGroup)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "slow-" + hex.EncodeToString(b) + ".pdf"
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name), bytes.NewReader(onePagePDF(20)), ferryReq, c)
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })
	for range 2 {
		res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?path="+q(name), nil, nil, c)
		if res.StatusCode != 503 || problemCode(t, res) != "preview_timeout" {
			t.Fatalf("past the job's limit: %d", res.StatusCode)
		}
	}
	_ = filepath.WalkDir(cfg.CacheDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			t.Errorf("cached after a timeout: %s", p)
		}
		return nil
	})
	if n := h.api.failures.len(); n != 0 {
		t.Errorf("a timeout remembered as a failure (%d)", n)
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

// A HEIC photo's thumbnail through the API: the sample from gen2brain/heic's
// test data, fetched like a module (internal/preview has the variants).
func TestHEICThumbnail(t *testing.T) {
	addr := os.Getenv("FERRY_TEST_SMB_SERVER")
	if addr == "" {
		t.Skip("FERRY_TEST_SMB_SERVER is not set (make dev-samba)")
	}
	out, err := exec.Command("go", "mod", "download", "-json", "github.com/gen2brain/heic@v0.4.9").Output()
	if err != nil {
		t.Skipf("the HEIC sample cannot be fetched: %v", err)
	}
	var m struct{ Dir string }
	_ = json.Unmarshal(out, &m)
	photo, err := os.ReadFile(filepath.Join(m.Dir, "testdata", "test.heic"))
	if err != nil {
		t.Skip(err)
	}
	cfg := testConfig()
	cfg.SMBServer = addr
	cfg.MaxUpload = 8 << 20
	cfg.SMBConnections = 3
	cfg.PreviewHEIC = true
	cfg.PreviewHEICMemory = 96 << 20
	cfg.PreviewConcurrency = 1
	cfg.ThumbnailMaxPixels = 50_000_000
	cfg.WASMDir = t.TempDir()
	if err := preview.Precompile(cfg.WASMDir); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, cfg, smb.LoginGroup)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "heic-" + hex.EncodeToString(b)
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c)
	})
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/IMG_2117.HEIC"), bytes.NewReader(photo), ferryReq, c)

	info := decode[map[string]any](t, h.do(t, "GET", "/api/v1/info", nil, nil, nil))
	if p, _ := info["previews"].([]any); len(p) != 1 || p[0] != "heic" {
		t.Fatalf("info's previews: %v", info["previews"])
	}
	res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=160&path="+q(dir+"/IMG_2117.HEIC"), nil, nil, c)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("a photo's thumbnail: %d", res.StatusCode)
	}
	img, err := jpeg.Decode(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 160 || b.Dy() != 160 {
		t.Fatalf("the 1346² photo in 160: %v", b)
	}
	h.api.cfg.ThumbnailMaxPixels = 1_000_000
	if res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=640&path="+q(dir+"/IMG_2117.HEIC"), nil, nil, c); res.StatusCode != 413 {
		t.Fatalf("over the pixel bound: %d", res.StatusCode)
	}
	// The 640 that was too large is remembered for the 640 only: the 320,
	// which a phone's photo makes from its thumbnail item, is still made.
	// (The bound is raised here so that this photo, which has no such item,
	// stands in for one that has.)
	if h.api.failures.len() != 1 {
		t.Fatalf("%d failures remembered", h.api.failures.len())
	}
	h.api.cfg.ThumbnailMaxPixels = 50_000_000
	if res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=320&path="+q(dir+"/IMG_2117.HEIC"), nil, nil, c); res.StatusCode != 200 {
		t.Fatalf("the 320 after the 640 failed: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=640&path="+q(dir+"/IMG_2117.HEIC"), nil, nil, c); res.StatusCode != 413 {
		t.Fatalf("the 640, remembered: %d", res.StatusCode)
	}
	if used, _ := h.api.ThumbnailMemory(); used != 0 {
		t.Fatalf("%d bytes still taken from the budget", used)
	}
}
