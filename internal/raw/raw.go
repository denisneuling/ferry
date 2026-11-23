// Package raw finds the JPEG preview a camera writes into its raw files, so
// that a raw photo has a thumbnail and a preview without decoding the
// sensor's data: CR2, NEF, ARW, DNG, PEF, ORF and RW2, which are TIFF
// underneath, and Fujifilm's RAF, which says where its JPEG is in its
// header.
//
// The bytes come from whoever put the file on the share, and this runs
// inside ferry, outside any sandbox: every offset and length is checked
// against the file's size before anything is read or allocated, every
// count is bounded, and a preview is taken only when its first bytes are a
// JPEG Go can decode — never the lossless JPEG some formats keep the
// sensor's data in.
package raw

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/jpeg"
	"io"
)

// ErrNoPreview is a file that is no raw photo ferry reads, or one without
// a JPEG preview it can use.
var ErrNoPreview = errors.New("raw: no JPEG preview")

// Preview is where a raw file keeps its JPEG, and how the photo is turned
// (EXIF orientation, 1–8; 0 when the container does not say, and the
// JPEG's own orientation stands).
type Preview struct {
	Offset, Length int64
	Orientation    int
}

// Bounds on what one look into a file may do.
const (
	maxIFDs       = 32       // IFDs visited in all, sub-IFDs included
	maxEntries    = 1000     // entries read from one IFD
	maxSubIFDs    = 8        // sub-IFDs followed from one IFD
	minPreview    = 512      // a JPEG smaller than this is no preview worth showing
	headerPeek    = 64 << 10 // bytes read of a candidate to check it is a JPEG Go decodes
	maxCandidates = 16
)

// Is says whether head, a file's first bytes, looks like a raw photo of a
// format this package reads. Plain TIFF matches too; Find then finds no
// preview in it, and the file is read as the image it is.
func Is(head []byte) bool {
	return isRAF(head) || tiffOrder(head) != nil
}

// Find returns the largest usable JPEG preview in a raw photo of size
// bytes.
func Find(r io.ReaderAt, size int64) (Preview, error) {
	head := make([]byte, min(size, 160))
	if _, err := r.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return Preview{}, err
	}
	if isRAF(head) {
		return findRAF(r, size, head)
	}
	order := tiffOrder(head)
	if order == nil {
		return Preview{}, ErrNoPreview
	}
	return findTIFF(r, size, order, head)
}

func isRAF(head []byte) bool {
	return bytes.HasPrefix(head, []byte("FUJIFILMCCD-RAW "))
}

// tiffOrder is the byte order of a TIFF-like header, or nil: TIFF (42),
// Olympus ORF ("RO", "RS") and Panasonic RW2 (0x55) use their own magic.
func tiffOrder(head []byte) binary.ByteOrder {
	if len(head) < 8 {
		return nil
	}
	var o binary.ByteOrder
	switch string(head[:2]) {
	case "II":
		o = binary.LittleEndian
	case "MM":
		o = binary.BigEndian
	default:
		return nil
	}
	switch o.Uint16(head[2:4]) {
	case 42, 0x4F52, 0x5352, 0x55:
		return o
	}
	return nil
}

// findRAF reads RAF's header: the JPEG's offset and length, big-endian, at
// 84 and 88. The JPEG keeps its own orientation.
func findRAF(r io.ReaderAt, size int64, head []byte) (Preview, error) {
	if len(head) < 92 {
		return Preview{}, ErrNoPreview
	}
	off := int64(binary.BigEndian.Uint32(head[84:88]))
	n := int64(binary.BigEndian.Uint32(head[88:92]))
	p := Preview{Offset: off, Length: n}
	if !inside(p, size) || !isJPEG(r, p) {
		return Preview{}, ErrNoPreview
	}
	return p, nil
}

// TIFF tags this package reads.
const (
	tagNewSubFileType = 0x00FE
	tagCompression    = 0x0103
	tagStripOffsets   = 0x0111
	tagOrientation    = 0x0112
	tagStripCounts    = 0x0117
	tagSubIFDs        = 0x014A
	tagJPEGOffset     = 0x0201
	tagJPEGLength     = 0x0202
	tagRW2Preview     = 0x002E // Panasonic: the JPEG itself, as an undefined blob
)

type entry struct {
	tag, typ uint16
	count    uint32
	value    []byte // the four bytes of the value or its offset
}

func findTIFF(r io.ReaderAt, size int64, o binary.ByteOrder, head []byte) (Preview, error) {
	queue := []int64{int64(o.Uint32(head[4:8]))}
	seen := map[int64]bool{}
	orientation := 0
	var candidates []Preview
	for len(queue) > 0 && len(seen) < maxIFDs {
		at := queue[0]
		queue = queue[1:]
		if at < 8 || at+2 > size || seen[at] {
			continue
		}
		first := len(seen) == 0
		seen[at] = true
		entries, next, err := readIFD(r, size, o, at)
		if err != nil {
			continue // a broken IFD: the others may still hold a preview
		}
		var (
			jpegOff, jpegLen, strip, stripLen int64
			compression, subType              uint32 = 0, 0
			strips                                   = false
		)
		for _, e := range entries {
			switch e.tag {
			case tagOrientation:
				if first {
					if v := int(value(o, e)); v >= 1 && v <= 8 {
						orientation = v
					}
				}
			case tagJPEGOffset:
				jpegOff = int64(value(o, e))
			case tagJPEGLength:
				jpegLen = int64(value(o, e))
			case tagCompression:
				compression = value(o, e)
			case tagNewSubFileType:
				subType = value(o, e)
			case tagStripOffsets:
				if e.count == 1 {
					strip, strips = int64(value(o, e)), true
				}
			case tagStripCounts:
				if e.count == 1 {
					stripLen = int64(value(o, e))
				}
			case tagSubIFDs:
				queue = append(queue, offsets(r, size, o, e, maxSubIFDs)...)
			case tagRW2Preview:
				if e.typ == 7 && e.count > 4 {
					candidates = append(candidates, Preview{Offset: int64(o.Uint32(e.value)), Length: int64(e.count)})
				}
			}
		}
		if jpegOff > 0 && jpegLen > 0 {
			candidates = append(candidates, Preview{Offset: jpegOff, Length: jpegLen})
		}
		// A JPEG strip: a preview (a reduced image, or CR2's first IFD),
		// or the sensor's lossless JPEG, which isJPEG refuses.
		if strips && stripLen > 0 && (compression == 6 || compression == 7) && (subType&1 == 1 || first) {
			candidates = append(candidates, Preview{Offset: strip, Length: stripLen})
		}
		if next > 0 {
			queue = append(queue, next)
		}
		if len(candidates) > maxCandidates {
			break
		}
	}
	best := Preview{}
	for _, c := range candidates {
		if c.Length > best.Length && inside(c, size) && isJPEG(r, c) {
			best = c
		}
	}
	if best.Length == 0 {
		return Preview{}, ErrNoPreview
	}
	best.Orientation = orientation
	return best, nil
}

// readIFD reads one IFD's entries and the offset of the next.
func readIFD(r io.ReaderAt, size int64, o binary.ByteOrder, at int64) ([]entry, int64, error) {
	var nb [2]byte
	if _, err := r.ReadAt(nb[:], at); err != nil {
		return nil, 0, err
	}
	n := int64(o.Uint16(nb[:]))
	if n == 0 || n > maxEntries || at+2+12*n+4 > size {
		return nil, 0, ErrNoPreview
	}
	buf := make([]byte, 12*n+4)
	if _, err := r.ReadAt(buf, at+2); err != nil {
		return nil, 0, err
	}
	out := make([]entry, 0, n)
	for i := int64(0); i < n; i++ {
		b := buf[12*i : 12*i+12]
		out = append(out, entry{tag: o.Uint16(b[0:2]), typ: o.Uint16(b[2:4]), count: o.Uint32(b[4:8]), value: b[8:12]})
	}
	return out, int64(o.Uint32(buf[12*n:])), nil
}

// value is a single SHORT or LONG entry's value.
func value(o binary.ByteOrder, e entry) uint32 {
	if e.count != 1 {
		return 0
	}
	switch e.typ {
	case 3: // SHORT
		return uint32(o.Uint16(e.value))
	case 4, 13: // LONG, IFD
		return o.Uint32(e.value)
	}
	return 0
}

// offsets are a LONG or IFD entry's values: in the entry when one, else at
// the offset it holds; at most max of them.
func offsets(r io.ReaderAt, size int64, o binary.ByteOrder, e entry, max int) []int64 {
	if e.typ != 4 && e.typ != 13 || e.count == 0 {
		return nil
	}
	if e.count == 1 {
		return []int64{int64(o.Uint32(e.value))}
	}
	n := min(int64(e.count), int64(max))
	at := int64(o.Uint32(e.value))
	if at < 0 || at+4*n > size {
		return nil
	}
	buf := make([]byte, 4*n)
	if _, err := r.ReadAt(buf, at); err != nil {
		return nil
	}
	out := make([]int64, 0, n)
	for i := int64(0); i < n; i++ {
		out = append(out, int64(o.Uint32(buf[4*i:])))
	}
	return out
}

func inside(p Preview, size int64) bool {
	return p.Offset > 0 && p.Length >= minPreview && p.Offset <= size && p.Length <= size-p.Offset
}

// isJPEG reads the candidate's start and checks it is a JPEG Go decodes:
// baseline or progressive, not lossless.
func isJPEG(r io.ReaderAt, p Preview) bool {
	b := make([]byte, min(p.Length, headerPeek))
	if _, err := r.ReadAt(b, p.Offset); err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	if len(b) < 3 || b[0] != 0xFF || b[1] != 0xD8 || b[2] != 0xFF {
		return false
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	return err == nil && cfg.Width > 0 && cfg.Height > 0
}
