package thumb

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// FuzzMake holds Make to its promises on any bytes, since every uploaded
// image reaches it inside the process: no panic, no error but the two it
// names, the same refusal Estimate made from the header, and a JPEG that
// fits the size and is never larger than the original. The pixel limit is
// small (64 Ki pixels) so that the fuzzer spends its time on the parsers,
// not on decoding large images, and cannot exhaust the runner's memory.
func FuzzMake(f *testing.F) {
	f.Add(encodeJPEG(f, halves(16, 8), 0))
	f.Add(encodeJPEG(f, halves(16, 8), 6))
	var buf bytes.Buffer
	transparent := image.NewNRGBA(image.Rect(0, 0, 9, 5))
	transparent.Set(1, 1, color.NRGBA{200, 10, 10, 128})
	if err := png.Encode(&buf, transparent); err != nil {
		f.Fatal(err)
	}
	f.Add(append([]byte{}, buf.Bytes()...))
	buf.Reset()
	if err := gif.Encode(&buf, halves(7, 7), nil); err != nil {
		f.Fatal(err)
	}
	f.Add(append([]byte{}, buf.Bytes()...))
	buf.Reset()
	if err := bmp.Encode(&buf, halves(6, 3)); err != nil {
		f.Fatal(err)
	}
	f.Add(append([]byte{}, buf.Bytes()...))
	buf.Reset()
	if err := tiff.Encode(&buf, halves(6, 4), nil); err != nil {
		f.Fatal(err)
	}
	f.Add(append([]byte{}, buf.Bytes()...))
	// A 1×1 lossless WebP: x/image has no WebP encoder.
	f.Add([]byte("RIFF\x1a\x00\x00\x00WEBPVP8L\x0d\x00\x00\x00\x2f\x00\x00\x00\x10\x07\x10\x11\x11\x88\x88\xfe\x07\x00"))
	f.Add([]byte("<svg xmlns='http://www.w3.org/2000/svg'/>"))

	const size, maxPixels = 64, 1 << 16
	f.Fuzz(func(t *testing.T, b []byte) {
		_, estimated := Estimate(b, int64(len(b)), maxPixels)
		out, err := Make(b, size, maxPixels)
		if estimated != nil {
			if err != estimated {
				t.Fatalf("Estimate refused with %v, Make answered %v", estimated, err)
			}
			return
		}
		if err != nil {
			if !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrTooLarge) {
				t.Fatalf("an error of its own: %v", err)
			}
			return
		}
		cfg, _, _ := image.DecodeConfig(bytes.NewReader(b))
		got, err := jpeg.DecodeConfig(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("not a JPEG: %v", err)
		}
		long := max(cfg.Width, cfg.Height)
		if got.Width > size || got.Height > size || max(got.Width, got.Height) > long {
			t.Fatalf("%dx%d from %dx%d", got.Width, got.Height, cfg.Width, cfg.Height)
		}
	})
}

// FuzzOrientation feeds the EXIF reader directly, which Make reaches only
// behind a JPEG that decodes: whatever the segment says, the answer is one
// of the eight orientations.
func FuzzOrientation(f *testing.F) {
	f.Add(encodeJPEG(f, halves(8, 8), 6)[:64])
	f.Add([]byte("\xFF\xD8\xFF\xE1\x00\x10Exif\x00\x00MM\x00*\xFF\xFF\xFF\xF0"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if o := exifOrientation(b); o < 1 || o > 8 {
			t.Fatalf("orientation %d", o)
		}
	})
}
