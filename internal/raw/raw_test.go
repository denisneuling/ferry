package raw

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// smallJPEG is a w×h JPEG, as a camera's preview stands in for.
func smallJPEG(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type ifdEntry struct {
	tag, typ uint16
	count    uint32
	value    uint32
}

// tiffFile writes a little-endian TIFF: IFDs at the offsets given, each a
// list of entries, and blobs at theirs.
func tiffFile(size int, ifds map[int][]ifdEntry, next map[int]int, blobs map[int][]byte) []byte {
	b := make([]byte, size)
	copy(b, "II*\x00")
	binary.LittleEndian.PutUint32(b[4:], 8)
	for at, es := range ifds {
		binary.LittleEndian.PutUint16(b[at:], uint16(len(es)))
		for i, e := range es {
			p := at + 2 + 12*i
			binary.LittleEndian.PutUint16(b[p:], e.tag)
			binary.LittleEndian.PutUint16(b[p+2:], e.typ)
			binary.LittleEndian.PutUint32(b[p+4:], e.count)
			binary.LittleEndian.PutUint32(b[p+8:], e.value)
		}
		binary.LittleEndian.PutUint32(b[at+2+12*len(es):], uint32(next[at]))
	}
	for at, blob := range blobs {
		copy(b[at:], blob)
	}
	return b
}

// A NEF or CR2 the way they keep it: a small preview in IFD0 by the JPEG
// tags, a larger one in a sub-IFD as a JPEG strip, the orientation in IFD0.
func nefLike(t testing.TB) ([]byte, int, int) {
	small, large := smallJPEG(t, 40, 30), smallJPEG(t, 160, 120)
	smallAt, largeAt := 1000, 4000
	f := tiffFile(largeAt+len(large)+16, map[int][]ifdEntry{
		8: {
			{tagOrientation, 3, 1, 6},
			{tagSubIFDs, 4, 1, 400},
			{tagJPEGOffset, 4, 1, uint32(smallAt)},
			{tagJPEGLength, 4, 1, uint32(len(small))},
		},
		400: {
			{tagNewSubFileType, 4, 1, 1},
			{tagCompression, 3, 1, 6},
			{tagStripOffsets, 4, 1, uint32(largeAt)},
			{tagStripCounts, 4, 1, uint32(len(large))},
		},
	}, nil, map[int][]byte{smallAt: small, largeAt: large})
	return f, largeAt, len(large)
}

func TestFindTIFF(t *testing.T) {
	f, at, n := nefLike(t)
	if !Is(f) {
		t.Fatal("not taken for a raw photo")
	}
	p, err := Find(bytes.NewReader(f), int64(len(f)))
	if err != nil || p.Offset != int64(at) || p.Length != int64(n) || p.Orientation != 6 {
		t.Fatalf("got %+v, %v; want the larger preview at %d+%d, turned 6", p, err, at, n)
	}
}

// A JPEG strip that is no JPEG Go decodes — the sensor's lossless data —
// is never taken, and a plain TIFF has no preview.
func TestFindRefuses(t *testing.T) {
	notJPEG := append([]byte{0xFF, 0xD8, 0xFF, 0xC3}, make([]byte, 2000)...) // SOF3: lossless
	f := tiffFile(5000, map[int][]ifdEntry{
		8: {
			{tagCompression, 3, 1, 7},
			{tagStripOffsets, 4, 1, 1000},
			{tagStripCounts, 4, 1, uint32(len(notJPEG))},
		},
	}, nil, map[int][]byte{1000: notJPEG})
	if _, err := Find(bytes.NewReader(f), int64(len(f))); err != ErrNoPreview {
		t.Fatalf("a lossless strip: %v", err)
	}
	// Offsets beyond the file, and an IFD that points to itself.
	f = tiffFile(600, map[int][]ifdEntry{
		8: {{tagJPEGOffset, 4, 1, 500}, {tagJPEGLength, 4, 1, 1 << 30}, {tagSubIFDs, 4, 1, 8}},
	}, map[int]int{8: 8}, nil)
	if _, err := Find(bytes.NewReader(f), int64(len(f))); err != ErrNoPreview {
		t.Fatalf("hostile offsets: %v", err)
	}
	if Is([]byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("a PNG is taken for a raw photo")
	}
}

func rafLike(t testing.TB) ([]byte, int, int) {
	j := smallJPEG(t, 120, 80)
	at := 200
	f := make([]byte, at+len(j))
	copy(f, "FUJIFILMCCD-RAW 0201FF383501")
	binary.BigEndian.PutUint32(f[84:], uint32(at))
	binary.BigEndian.PutUint32(f[88:], uint32(len(j)))
	copy(f[at:], j)
	return f, at, len(j)
}

func TestFindRAF(t *testing.T) {
	f, at, n := rafLike(t)
	p, err := Find(bytes.NewReader(f), int64(len(f)))
	if err != nil || p.Offset != int64(at) || p.Length != int64(n) || p.Orientation != 0 {
		t.Fatalf("got %+v, %v", p, err)
	}
}

// countingReaderAt counts what is read, to hold Find to small reads.
type countingReaderAt struct {
	r    *bytes.Reader
	read int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.read += int64(n)
	return n, err
}

// FuzzFind feeds Find what its offsets and counts make of arbitrary bytes:
// no panic, a preview only inside the file and a JPEG, and the reads
// bounded by the IFDs and candidates it may look at, whatever the file
// claims.
func FuzzFind(f *testing.F) {
	nef, _, _ := nefLike(f)
	raf, _, _ := rafLike(f)
	f.Add(nef)
	f.Add(raf)
	f.Add([]byte("IIRO\x08\x00\x00\x00"))
	f.Add([]byte("IIU\x00\x08\x00\x00\x00"))
	const most = maxIFDs*(2+12*maxEntries+4+4*maxSubIFDs) + (maxCandidates+maxIFDs*2)*headerPeek + 160
	f.Fuzz(func(t *testing.T, b []byte) {
		r := &countingReaderAt{r: bytes.NewReader(b)}
		p, err := Find(r, int64(len(b)))
		if r.read > most {
			t.Fatalf("read %d bytes", r.read)
		}
		if err != nil {
			return
		}
		if p.Offset <= 0 || p.Length < minPreview || p.Offset+p.Length > int64(len(b)) {
			t.Fatalf("a preview at %d+%d in %d bytes", p.Offset, p.Length, len(b))
		}
		if b[p.Offset] != 0xFF || b[p.Offset+1] != 0xD8 {
			t.Fatal("a preview that is no JPEG")
		}
		if p.Orientation < 0 || p.Orientation > 8 {
			t.Fatalf("orientation %d", p.Orientation)
		}
	})
}
