package api

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/jpeg"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/metapatrol/ferry/internal/metrics"
)

// thumbnailOutcome reads ferry_thumbnails_total for one outcome from the
// metrics the harness's API records.
func thumbnailOutcome(t *testing.T, m *metrics.Metrics, outcome string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	want := `ferry_thumbnails_total{outcome="` + outcome + `"} `
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), want); ok {
			n, _ := strconv.Atoi(v)
			return n
		}
	}
	return 0
}

// A file that makes no thumbnail is decoded once and then answered from
// memory, with the same answer; changed, it is decoded again. Whoever may
// not open the file learns nothing from what was remembered.
func TestRememberedFailures(t *testing.T) {
	h := sambaHarness(t)
	m := metrics.New("test", "test", metrics.Gauges{})
	h.api.metrics = m
	alex := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "broken-" + hex.EncodeToString(b) + ".jpg"
	path := "/api/v1/shares/alex/thumbnail?path=" + q(name)
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, alex) })
	// A JPEG's start, then nothing a decoder takes.
	broken := append([]byte{0xff, 0xd8, 0xff, 0xe0}, bytes.Repeat([]byte{0x42}, 4000)...)
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name), bytes.NewReader(broken), ferryReq, alex)

	for i := range 3 {
		res := h.do(t, "GET", path, nil, nil, alex)
		if res.StatusCode != 415 || problemCode(t, res) != "not_an_image" {
			t.Fatalf("request %d: %d", i, res.StatusCode)
		}
	}
	if n := thumbnailOutcome(t, m, "unsupported"); n != 1 {
		t.Errorf("decoded %d times", n)
	}
	if n := thumbnailOutcome(t, m, "remembered"); n != 2 {
		t.Errorf("answered from memory %d times", n)
	}

	// sam may not open alex's share: Samba's answer, not the remembered one.
	sam := h.login(t, "sam", "sam-test-password")
	if res := h.do(t, "GET", path, nil, nil, sam); res.StatusCode != 403 {
		t.Fatalf("sam: %d", res.StatusCode)
	}

	var good bytes.Buffer
	if err := jpeg.Encode(&good, image.NewGray(image.Rect(0, 0, 400, 300)), nil); err != nil {
		t.Fatal(err)
	}
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name)+"&overwrite=true", bytes.NewReader(good.Bytes()), ferryReq, alex)
	if res := h.do(t, "GET", path, nil, nil, alex); res.StatusCode != 200 {
		t.Fatalf("the file mended: %d", res.StatusCode)
	}
}
