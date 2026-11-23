package api

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"
)

// A raw photo's thumbnail is made from the JPEG preview it carries, turned
// as the raw file's IFD0 says.
func TestRawThumbnail(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	img := image.NewRGBA(image.Rect(0, 0, 200, 100)) // landscape, as a camera stores it
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{200, uint8(x), 40, 255})
		}
	}
	var j bytes.Buffer
	if err := jpeg.Encode(&j, img, nil); err != nil {
		t.Fatal(err)
	}
	const at = 512
	f := make([]byte, at+j.Len())
	copy(f, "II*\x00\x08\x00\x00\x00")
	le := binary.LittleEndian
	le.PutUint16(f[8:], 3)
	for i, e := range [][3]uint32{{0x0112, 3, 6}, {0x0201, 4, at}, {0x0202, 4, uint32(j.Len())}} {
		p := 10 + 12*i
		le.PutUint16(f[p:], uint16(e[0]))
		le.PutUint16(f[p+2:], uint16(e[1]))
		le.PutUint32(f[p+4:], 1)
		le.PutUint32(f[p+8:], e[2])
	}
	copy(f[at:], j.Bytes())
	name := "raw-" + time.Now().Format("150405.000") + ".nef"
	if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name), bytes.NewReader(f), ferryReq, c); res.StatusCode != 201 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })
	res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=160&path="+q(name), nil, nil, c)
	if res.StatusCode != 200 {
		t.Fatalf("thumbnail: %d", res.StatusCode)
	}
	cfg, err := jpeg.DecodeConfig(res.Body)
	// Orientation 6: the 200×100 preview stands upright, 80×160.
	if err != nil || cfg.Width != 80 || cfg.Height != 160 {
		t.Fatalf("thumbnail %dx%d, %v", cfg.Width, cfg.Height, err)
	}
}
