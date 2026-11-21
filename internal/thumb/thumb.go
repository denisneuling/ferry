// Package thumb makes thumbnails of images in memory: decode, turn upright,
// scale, encode as JPEG. Nothing is written anywhere; ferry makes a
// thumbnail every time it is asked for one, and the browser caches it.
//
// Every input is bounded before it is decoded: the caller limits the bytes,
// and Make reads the image's dimensions first and refuses one with more
// pixels than allowed — a small file can claim a huge image and exhaust
// memory on decoding (a decompression bomb).
package thumb

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // registered decoders
	"image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var (
	// ErrUnsupported is an image format Make cannot decode, or not an image.
	ErrUnsupported = errors.New("thumb: not an image ferry can read")
	// ErrTooLarge is an image with more pixels than allowed.
	ErrTooLarge = errors.New("thumb: the image is too large")
)

// Make returns a JPEG of the image in src that fits in a size×size square,
// upright, never larger than the original. Images with transparency are put
// on white.
func Make(src []byte, size, maxPixels int) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, ErrUnsupported
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxPixels/cfg.Height {
		return nil, ErrTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, ErrUnsupported
	}
	orientation := 1
	if format == "jpeg" {
		orientation = exifOrientation(src)
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if orientation >= 5 {
		// Rotated a quarter turn: the thumbnail's width is the source's height.
		w, h = h, w
	}
	tw, th := fit(w, h, size)
	if orientation >= 5 {
		tw, th = th, tw
	}

	// A JPEG is first reduced by whole blocks on its own planes: averaging
	// k×k pixels is cheap and does not alias, and it leaves CatmullRom a
	// small image to finish. On a 12-megapixel photo that halves the time
	// (BenchmarkPhoto).
	if y, ok := img.(*image.YCbCr); ok {
		if k := min(w/(2*tw), h/(2*th)); orientation >= 5 {
			if k2 := min(b.Dx()/(2*th), b.Dy()/(2*tw)); k2 >= 2 {
				img = boxYCbCr(y, k2)
			}
		} else if k >= 2 {
			img = boxYCbCr(y, k)
		}
		b = img.Bounds()
	}

	// Scale first, in the source's orientation, then turn the small result.
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	out := orient(dst, orientation)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// boxYCbCr shrinks a YCbCr image by an integer factor k, averaging k×k
// blocks of each plane. The chroma planes keep their subsampling, so the
// result is the same kind of image, k times smaller.
func boxYCbCr(src *image.YCbCr, k int) *image.YCbCr {
	sb := src.Bounds()
	w, h := sb.Dx()/k, sb.Dy()/k
	dst := image.NewYCbCr(image.Rect(0, 0, w, h), src.SubsampleRatio)
	box(dst.Y, dst.YStride, w, h, src.Y[src.YOffset(sb.Min.X, sb.Min.Y):], src.YStride, k)
	cw, ch := chromaSize(w, h, src.SubsampleRatio)
	co := src.COffset(sb.Min.X, sb.Min.Y)
	box(dst.Cb, dst.CStride, cw, ch, src.Cb[co:], src.CStride, k)
	box(dst.Cr, dst.CStride, cw, ch, src.Cr[co:], src.CStride, k)
	return dst
}

// box writes into dst (w×h, stride ds) the averages of k×k blocks of src
// (stride ss), staying inside src's plane.
func box(dst []byte, ds, w, h int, src []byte, ss, k int) {
	rows := (len(src) + ss - 1) / ss
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sum, n := 0, 0
			for dy := 0; dy < k && y*k+dy < rows; dy++ {
				row := (y*k + dy) * ss
				for dx := 0; dx < k; dx++ {
					if i := row + x*k + dx; i < len(src) && x*k+dx < ss {
						sum += int(src[i])
						n++
					}
				}
			}
			if n > 0 {
				dst[y*ds+x] = byte(sum / n)
			}
		}
	}
}

// chromaSize is the size of the chroma planes of a w×h image.
func chromaSize(w, h int, r image.YCbCrSubsampleRatio) (int, int) {
	switch r {
	case image.YCbCrSubsampleRatio422:
		return (w + 1) / 2, h
	case image.YCbCrSubsampleRatio420:
		return (w + 1) / 2, (h + 1) / 2
	case image.YCbCrSubsampleRatio440:
		return w, (h + 1) / 2
	case image.YCbCrSubsampleRatio411:
		return (w + 3) / 4, h
	case image.YCbCrSubsampleRatio410:
		return (w + 3) / 4, (h + 1) / 2
	}
	return w, h
}

// fit scales w×h into a size×size square, keeping the aspect ratio and
// never enlarging.
func fit(w, h, size int) (int, int) {
	if w <= size && h <= size {
		return w, h
	}
	if w >= h {
		return size, max(1, h*size/w)
	}
	return max(1, w*size/h), size
}

// orient applies an EXIF orientation (1–8) to img.
func orient(img *image.RGBA, o int) image.Image {
	if o < 2 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2: // mirrored
				nx, ny = w-1-x, y
			case 3: // upside down
				nx, ny = w-1-x, h-1-y
			case 4: // mirrored, upside down
				nx, ny = x, h-1-y
			case 5: // mirrored, a quarter turn counter-clockwise
				nx, ny = y, x
			case 6: // a quarter turn clockwise
				nx, ny = h-1-y, x
			case 7: // mirrored, a quarter turn clockwise
				nx, ny = h-1-y, w-1-x
			case 8: // a quarter turn counter-clockwise
				nx, ny = y, w-1-x
			}
			out.SetRGBA(nx, ny, img.RGBAAt(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

// exifOrientation reads the orientation tag (0x0112) from a JPEG's EXIF
// segment, and returns 1 — upright — when there is none or it cannot be
// read. It reads only as far as it must, and bounds every offset by the
// segment: the bytes come from whoever uploaded the file.
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data or end: no EXIF before it
			return 1
		}
		n := int(b[i+2])<<8 | int(b[i+3])
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(t[:2]) {
	case "II":
		u16 = func(p []byte) int { return int(p[0]) | int(p[1])<<8 }
		u32 = func(p []byte) int { return int(p[0]) | int(p[1])<<8 | int(p[2])<<16 | int(p[3])<<24 }
	case "MM":
		u16 = func(p []byte) int { return int(p[0])<<8 | int(p[1]) }
		u32 = func(p []byte) int { return int(p[0])<<24 | int(p[1])<<16 | int(p[2])<<8 | int(p[3]) }
	default:
		return 1
	}
	ifd := u32(t[4:8])
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	count := u16(t[ifd : ifd+2])
	for k := 0; k < count && k < 512; k++ {
		e := ifd + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if u16(t[e:e+2]) == 0x0112 {
			if o := u16(t[e+8 : e+10]); o >= 1 && o <= 8 {
				return o
			}
			return 1
		}
	}
	return 1
}
