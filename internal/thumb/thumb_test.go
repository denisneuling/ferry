package thumb

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// halves is a w×h image, red on the left half and blue on the right.
func halves(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{220, 20, 20, 255}
			if x >= w/2 {
				c = color.RGBA{20, 20, 220, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t testing.TB, img image.Image, orientation int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if orientation == 0 {
		return b
	}
	// An APP1 Exif segment with one IFD0 entry: Orientation, SHORT, 1.
	tiff := []byte("II*\x00\x08\x00\x00\x00" + "\x01\x00" +
		"\x12\x01\x03\x00\x01\x00\x00\x00" + string([]byte{byte(orientation), 0, 0, 0}) +
		"\x00\x00\x00\x00")
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	app1 = append(app1, seg...)
	out := append([]byte{}, b[:2]...)
	out = append(out, app1...)
	return append(out, b[2:]...)
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 0x9000 && g < 0x5000 && b < 0x5000
}

func isBlue(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return b > 0x9000 && r < 0x5000 && g < 0x5000
}

func TestSize(t *testing.T) {
	out, err := Make(encodeJPEG(t, halves(800, 400), 0), 320, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if b := decode(t, out).Bounds(); b.Dx() != 320 || b.Dy() != 160 {
		t.Fatalf("thumbnail %v", b)
	}
	// Smaller than the box: never enlarged.
	out, err = Make(encodeJPEG(t, halves(100, 60), 0), 320, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if b := decode(t, out).Bounds(); b.Dx() != 100 || b.Dy() != 60 {
		t.Fatalf("small image %v", b)
	}
}

func TestOrientation(t *testing.T) {
	// The source is 400×200, red left, blue right. Where the red half ends
	// up says how it was turned.
	cases := []struct {
		o             int
		w, h          int
		redAt, blueAt image.Point
		name          string
	}{
		{1, 320, 160, image.Pt(40, 80), image.Pt(280, 80), "upright"},
		{3, 320, 160, image.Pt(280, 80), image.Pt(40, 80), "upside down"},
		{6, 160, 320, image.Pt(80, 40), image.Pt(80, 280), "a quarter turn clockwise"},
		{8, 160, 320, image.Pt(80, 280), image.Pt(80, 40), "a quarter turn counter-clockwise"},
		{2, 320, 160, image.Pt(280, 80), image.Pt(40, 80), "mirrored"},
	}
	for _, c := range cases {
		out, err := Make(encodeJPEG(t, halves(400, 200), c.o), 320, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		img := decode(t, out)
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("%s: %v", c.name, b)
			continue
		}
		if !isRed(img.At(c.redAt.X, c.redAt.Y)) || !isBlue(img.At(c.blueAt.X, c.blueAt.Y)) {
			t.Errorf("%s: red at %v is %v, blue at %v is %v", c.name, c.redAt, img.At(c.redAt.X, c.redAt.Y), c.blueAt, img.At(c.blueAt.X, c.blueAt.Y))
		}
	}
}

func TestTransparencyOnWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 50, 50)) // fully transparent
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, err := Make(buf.Bytes(), 320, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decode(t, out).At(25, 25).RGBA()
	if r < 0xF000 || g < 0xF000 || b < 0xF000 {
		t.Fatalf("not white: %x %x %x", r, g, b)
	}
}

// A small PNG whose header claims a gigapixel image is refused before it is
// decoded.
func TestDecompressionBomb(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// IHDR follows the 8-byte signature: length (4), type (4), width, height.
	binary.BigEndian.PutUint32(b[16:], 40000)
	binary.BigEndian.PutUint32(b[20:], 40000)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	if _, err := Make(b, 320, 50_000_000); err != ErrTooLarge {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
}

func TestNotAnImage(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("hello"), []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), {0xFF, 0xD8, 0xFF}} {
		if _, err := Make(b, 320, 1<<30); err != ErrUnsupported {
			t.Errorf("%q: %v", b, err)
		}
	}
}

// Malformed EXIF never panics and reads as upright.
func TestExifOrientationMalformed(t *testing.T) {
	good := encodeJPEG(t, halves(8, 8), 6)
	if o := exifOrientation(good); o != 6 {
		t.Fatalf("orientation %d", o)
	}
	for cut := 0; cut < 60 && cut < len(good); cut++ {
		_ = exifOrientation(good[:cut])
	}
	bad := append([]byte{}, good...)
	bad[2+4+6+4] = 0xFF // the IFD offset, now far beyond the segment
	if o := exifOrientation(bad); o != 1 {
		t.Fatalf("a broken offset gave %d", o)
	}
}
