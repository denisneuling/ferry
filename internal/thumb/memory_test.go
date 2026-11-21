package thumb

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// peakHeap runs f and returns the most heap it held above what was in use
// before, sampled every millisecond.
func peakHeap(f func()) uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	base := ms.HeapAlloc
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > base && m.HeapAlloc-base > peak.Load() {
				peak.Store(m.HeapAlloc - base)
			}
			time.Sleep(time.Millisecond)
		}
	}()
	f()
	close(stop)
	<-done
	return peak.Load()
}

// TestMemoryAtTheLimit measures the heap one thumbnail takes for the worst
// images at 50 megapixels, the default limit: docs/operations.md quotes it.
// It allocates gigabytes to make its inputs, so it runs only on request:
// FERRY_TEST_THUMB_MEMORY=1 go test -run MemoryAtTheLimit -v ./internal/thumb/
func TestMemoryAtTheLimit(t *testing.T) {
	if os.Getenv("FERRY_TEST_THUMB_MEMORY") == "" {
		t.Skip("set FERRY_TEST_THUMB_MEMORY=1 to measure")
	}
	const w, h = 8660, 5773 // 50 MP, 3:2
	type input struct {
		name string
		make func() []byte
	}
	gradient := func(set func(x, y int, v uint8)) {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				set(x, y, uint8((x+y)/64))
			}
		}
	}
	jpegOf := func(img image.Image) []byte {
		var b bytes.Buffer
		_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
		return b.Bytes()
	}
	pngOf := func(img image.Image) []byte {
		var b bytes.Buffer
		_ = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, img)
		return b.Bytes()
	}
	inputs := []input{
		{"JPEG 4:2:0", func() []byte {
			img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
			gradient(func(x, y int, v uint8) { img.Y[y*img.YStride+x] = v })
			return jpegOf(img)
		}},
		{"JPEG 4:4:4", func() []byte {
			img := image.NewRGBA(image.Rect(0, 0, w, h)) // the encoder writes 4:4:4 from RGBA
			gradient(func(x, y int, v uint8) { img.SetRGBA(x, y, color.RGBA{v, v, 255 - v, 255}) })
			return jpegOf(img)
		}},
		{"PNG RGBA 8-bit", func() []byte {
			img := image.NewNRGBA(image.Rect(0, 0, w, h))
			gradient(func(x, y int, v uint8) { img.SetNRGBA(x, y, color.NRGBA{v, 255 - v, v, 200}) })
			return pngOf(img)
		}},
		{"PNG RGBA 16-bit", func() []byte {
			img := image.NewNRGBA64(image.Rect(0, 0, w, h))
			gradient(func(x, y int, v uint8) {
				img.SetNRGBA64(x, y, color.NRGBA64{uint16(v) << 8, 0, 0xffff, 0xffff})
			})
			return pngOf(img)
		}},
	}
	for _, in := range inputs {
		src := in.make()
		runtime.GC()
		est, err := Estimate(src[:min(len(src), HeaderBytes)], int64(len(src)), 50_000_000)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		peak := peakHeap(func() { _, err = Make(src, 640, 50_000_000) })
		fmt.Printf("%-16s source %6.1f MB  peak heap %7.1f MB  estimate %7.1f MB  %v  err=%v\n",
			in.name, float64(len(src))/1e6, float64(peak)/1e6, float64(est)/1e6, time.Since(start).Round(time.Millisecond), err)
		if peak > uint64(est) {
			t.Errorf("%s: the peak exceeds the estimate", in.name)
		}
	}
}
