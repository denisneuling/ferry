package thumb

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// BenchmarkPhoto makes a thumbnail of a 12-megapixel JPEG, what a phone
// takes. It sets the default for how many thumbnails are made at once.
func BenchmarkPhoto(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	r := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.Intn(256))
	}
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	src := encodeJPEG(&testing.T{}, img, 6)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Make(src, 320, 50_000_000); err != nil {
			b.Fatal(err)
		}
	}
}
