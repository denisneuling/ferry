package meta

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strings"
)

var (
	// ErrNoCover is a recording without a picture ferry can find, or not a
	// recording at all.
	ErrNoCover = errors.New("meta: no cover")
	// ErrCoverTooLarge is a picture larger than the caller allows.
	ErrCoverTooLarge = errors.New("meta: the cover is too large")
)

// inMemory is the largest picture FindCover copies into memory itself: one
// stored in a way that is not a plain run of the file's bytes (an ID3 tag
// written with unsynchronisation, base64 in an Ogg comment). Such a copy
// takes up to about three times this — what is read, the packet, the
// decoded picture — before the caller's memory budget can count the
// picture, so it is kept small and passes the caller's gate first; covers
// are rarely more than a few hundred kilobytes.
const inMemory = 8 << 20

// catchPanics is turned off by the fuzz test, which wants to see them.
var catchPanics = true

// Cover is the picture a recording carries: Size bytes, readable with
// Reader. A plain run of the file is read from the file when it is asked
// for, not before.
type Cover struct {
	Size int64
	r    io.Reader
}

// Reader returns the picture's bytes, once.
func (c Cover) Reader() io.Reader { return c.r }

// IsAudio tells from a file's first bytes whether it is a format FindCover
// looks into: MP3 (with an ID3v2 tag), FLAC, MP4 and M4A, Ogg.
func IsAudio(h []byte) bool {
	return bytes.HasPrefix(h, []byte("ID3")) || bytes.HasPrefix(h, []byte("fLaC")) ||
		bytes.HasPrefix(h, []byte("OggS")) || (len(h) >= 8 && string(h[4:8]) == "ftyp")
}

// FindCover finds the picture a recording carries — the front cover when
// it says which one that is, the first otherwise — reading its tags'
// headers within Budget, and not the picture itself. A picture larger than
// most bytes is ErrCoverTooLarge.
//
// gate, when not nil, is called once before FindCover copies a picture into
// memory (see inMemory), and may wait; its error ends the search and is
// returned. The caller holds what gate gave until the picture is in its
// memory budget.
func FindCover(r io.ReaderAt, size, most int64, gate func() error) (c Cover, err error) {
	// The tags' headers within Budget; a picture held in memory (see
	// inMemory) on top, in base64 at worst.
	b := &budget{r: r, size: size, left: Budget + 2*inMemory, gate: gate}
	defer func() {
		// A parser's bug on a hostile file must not take the request down.
		if catchPanics {
			if recover() != nil {
				c, err = Cover{}, ErrNoCover
			}
		}
	}()
	h := b.at(0, 64)
	var pics []picture
	switch {
	case bytes.HasPrefix(h, []byte("ID3")):
		pics = id3Pictures(b, h)
	case bytes.HasPrefix(h, []byte("fLaC")):
		pics = flacPictures(b)
	case len(h) >= 8 && string(h[4:8]) == "ftyp":
		pics = mp4Pictures(b)
	case bytes.HasPrefix(h, []byte("OggS")):
		pics = oggPictures(b, most)
	}
	if b.gateErr != nil {
		return Cover{}, b.gateErr
	}
	var best *picture
	for i := range pics {
		if pics[i].size <= 0 {
			continue
		}
		if best == nil || (pics[i].front && !best.front) {
			best = &pics[i]
		}
	}
	switch {
	case best == nil, best.data == nil && best.size != tooLarge && best.off+best.size > size:
		// None, or one a cut-off file promises and does not hold.
		return Cover{}, ErrNoCover
	case best.size > most:
		return Cover{}, ErrCoverTooLarge
	case best.data != nil:
		return Cover{Size: int64(len(best.data)), r: bytes.NewReader(best.data)}, nil
	}
	return Cover{Size: best.size, r: io.NewSectionReader(r, best.off, best.size)}, nil
}

// picture is a candidate: a run of the file at off, or bytes already
// decoded into data.
type picture struct {
	off, size int64
	data      []byte
	front     bool
}

// tooLarge stands for a picture FindCover will not copy into memory; it is
// larger than any limit a caller passes.
const tooLarge = 1 << 62

// pictureType 3 is the front cover, in ID3's APIC and FLAC's PICTURE alike.
const frontCover = 3

// ── ID3v2 (MP3) ───────────────────────────────────────────────────────────

// id3Pictures walks the frames of an ID3v2.2, 2.3 or 2.4 tag by their
// headers and returns its APIC (PIC in 2.2) frames.
func id3Pictures(b *budget, h []byte) []picture {
	if len(h) < 10 {
		return nil
	}
	major, flags := h[3], h[5]
	tagSize := syncsafe(h[6:10])
	end := 10 + tagSize
	if end > b.size {
		end = b.size
	}
	if major == 2 && flags&0x40 != 0 {
		return nil // a compressed 2.2 tag, never seen in use
	}
	if flags&0x80 != 0 && major < 4 {
		// Unsynchronisation before 2.4 covers the frame headers too: undo it
		// on the whole tag in memory and walk that.
		if end-10 > inMemory {
			return []picture{{size: tooLarge}}
		}
		if !b.toMemory() {
			return nil
		}
		raw := b.at(10, int(end-10))
		d := append(make([]byte, 0, len(raw)+10), h[:10]...)
		d = append(d, bytes.ReplaceAll(raw, []byte{0xFF, 0x00}, []byte{0xFF})...)
		d[5] &^= 0x80
		inner := &budget{r: bytes.NewReader(d), size: int64(len(d)), left: int64(len(d)) * 2}
		pics := id3Pictures(inner, d)
		for i := range pics {
			if pics[i].data == nil && pics[i].size > 0 && pics[i].size != tooLarge {
				pics[i].data = d[pics[i].off : pics[i].off+pics[i].size]
			}
		}
		return pics
	}
	off := int64(10)
	if flags&0x40 != 0 && major >= 3 { // an extended header
		x := b.at(off, 4)
		if len(x) < 4 {
			return nil
		}
		n := int64(binary.BigEndian.Uint32(x))
		if major == 4 {
			n = syncsafe(x) // 2.4 counts the size field in
		} else {
			n += 4
		}
		off += n
	}
	tagUnsync := flags&0x80 != 0 // 2.4: every frame's data, not its header
	hdr, idLen := int64(10), 4
	if major == 2 {
		hdr, idLen = 6, 3
	}
	var pics []picture
	for frames := 0; off+hdr <= end && frames < 1024; frames++ {
		fh := b.at(off, int(hdr))
		if int64(len(fh)) < hdr || fh[0] == 0 {
			break // padding
		}
		id := string(fh[:idLen])
		var n int64
		var fflags byte
		switch major {
		case 2:
			n = int64(fh[3])<<16 | int64(fh[4])<<8 | int64(fh[5])
		case 3:
			n = int64(binary.BigEndian.Uint32(fh[4:]))
			fflags = fh[9]
		default:
			n = syncsafe(fh[4:8])
			fflags = fh[9]
		}
		body := off + hdr
		if n <= 0 || body+n > end {
			break
		}
		if id == "APIC" || id == "PIC" {
			if p, ok := id3Picture(b, major, fflags, tagUnsync, body, n); ok {
				pics = append(pics, p)
			}
		}
		off = body + n
	}
	return pics
}

// id3Picture reads one picture frame's header — encoding, MIME type (a
// three-letter format in 2.2), picture type, description — and returns where
// the picture starts.
func id3Picture(b *budget, major, fflags byte, tagUnsync bool, body, n int64) (picture, bool) {
	unsync := tagUnsync
	switch major {
	case 3:
		if fflags&0xC0 != 0 {
			return picture{}, false // compressed or encrypted
		}
		if fflags&0x20 != 0 { // a group identifier
			body, n = body+1, n-1
		}
	case 4:
		if fflags&0x0C != 0 {
			return picture{}, false // compressed or encrypted
		}
		if fflags&0x40 != 0 { // a group identifier
			body, n = body+1, n-1
		}
		if fflags&0x01 != 0 { // a data length indicator
			body, n = body+4, n-4
		}
		unsync = unsync || fflags&0x02 != 0
	}
	if n <= 0 {
		return picture{}, false
	}
	if unsync {
		// The frame's bytes are not the picture's: every 0xFF 0x00 stands for
		// 0xFF. Undo it in memory, within limits.
		if n > inMemory {
			return picture{size: tooLarge}, true
		}
		if !b.toMemory() {
			return picture{}, false
		}
		raw := b.at(body, int(n))
		if int64(len(raw)) < n {
			return picture{}, false
		}
		d := bytes.ReplaceAll(raw, []byte{0xFF, 0x00}, []byte{0xFF})
		skip, front, ok := id3PictureHeader(d, major)
		if !ok {
			return picture{}, false
		}
		return picture{data: d[skip:], size: int64(len(d) - skip), front: front}, true
	}
	// The header is short; a description is rarely more than a line.
	head := b.at(body, int(min(n, 4096)))
	skip, front, ok := id3PictureHeader(head, major)
	if !ok {
		return picture{}, false
	}
	return picture{off: body + int64(skip), size: n - int64(skip), front: front}, true
}

// id3PictureHeader returns how many bytes of a picture frame's body come
// before the picture, and whether it is the front cover.
func id3PictureHeader(d []byte, major byte) (int, bool, bool) {
	if len(d) < 2 {
		return 0, false, false
	}
	enc := d[0]
	p := 1
	if major == 2 {
		p += 3 // "JPG", "PNG"
	} else {
		i := bytes.IndexByte(d[p:], 0)
		if i < 0 {
			return 0, false, false
		}
		p += i + 1
	}
	if p >= len(d) {
		return 0, false, false
	}
	typ := d[p]
	p++
	// The description ends in a NUL, two in the UTF-16 encodings, aligned.
	if enc == 1 || enc == 2 {
		for ; p+1 < len(d); p += 2 {
			if d[p] == 0 && d[p+1] == 0 {
				return p + 2, typ == frontCover, true
			}
		}
		return 0, false, false
	}
	i := bytes.IndexByte(d[p:], 0)
	if i < 0 {
		return 0, false, false
	}
	return p + i + 1, typ == frontCover, true
}

func syncsafe(b []byte) int64 {
	return int64(b[0]&0x7F)<<21 | int64(b[1]&0x7F)<<14 | int64(b[2]&0x7F)<<7 | int64(b[3]&0x7F)
}

// ── FLAC ──────────────────────────────────────────────────────────────────

// flacPictures walks the metadata blocks by their headers and returns the
// PICTURE blocks.
func flacPictures(b *budget) []picture {
	var pics []picture
	for off, blocks := int64(4), 0; off+4 <= b.size && blocks < 1024; blocks++ {
		h := b.at(off, 4)
		if len(h) < 4 {
			break
		}
		last, typ := h[0]&0x80 != 0, h[0]&0x7F
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		body := off + 4
		if typ == 6 {
			if p, ok := flacPicture(b, body, n); ok {
				pics = append(pics, p)
			}
		}
		if last || typ == 127 {
			break
		}
		off = body + n
	}
	return pics
}

// flacPicture reads a PICTURE block's header: type, MIME type, description,
// dimensions, and the length of the picture that follows. Ogg carries the
// same structure, base64-encoded, so it parses bytes in memory too.
func flacPicture(b *budget, body, n int64) (picture, bool) {
	at := func(off int64, k int) []byte { return b.at(body+off, k) }
	return parsePictureBlock(at, n, body)
}

// parsePictureBlock reads the structure of a FLAC picture block through at,
// a reader of the block's bytes by offset, and returns the picture's place:
// base plus its offset in the block.
func parsePictureBlock(at func(off int64, n int) []byte, n, base int64) (picture, bool) {
	u32 := func(off int64) (int64, bool) {
		d := at(off, 4)
		if len(d) < 4 {
			return 0, false
		}
		return int64(binary.BigEndian.Uint32(d)), true
	}
	typ, ok := u32(0)
	if !ok {
		return picture{}, false
	}
	mime, ok := u32(4)
	if !ok || mime > 256 {
		return picture{}, false
	}
	desc, ok := u32(8 + mime)
	if !ok || desc > n {
		return picture{}, false
	}
	// Width, height, depth and colours: 16 bytes, then the length.
	p := 12 + mime + desc + 16
	size, ok := u32(p)
	if !ok || p+4+size > n {
		return picture{}, false
	}
	return picture{off: base + p + 4, size: size, front: typ == frontCover}, true
}

// ── MP4 and M4A ───────────────────────────────────────────────────────────

// mp4Pictures follows moov/udta/meta/ilst/covr, where iTunes and the tools
// after it keep a cover, and returns covr's data boxes.
func mp4Pictures(b *budget) []picture {
	moov, ok := find(boxes(b, 0, b.size), "moov")
	if !ok {
		return nil
	}
	udta, ok := find(boxes(b, moov.start, moov.end), "udta")
	if !ok {
		return nil
	}
	m, ok := find(boxes(b, udta.start, udta.end), "meta")
	if !ok {
		return nil
	}
	// meta is a full box in MP4 (a version and flags before its children)
	// and a plain one in QuickTime: what follows tells which.
	start := m.start
	if t := b.at(start+4, 4); len(t) == 4 && string(t) != "hdlr" {
		start += 4
	}
	ilst, ok := find(boxes(b, start, m.end), "ilst")
	if !ok {
		return nil
	}
	covr, ok := find(boxes(b, ilst.start, ilst.end), "covr")
	if !ok {
		return nil
	}
	var pics []picture
	for _, x := range boxes(b, covr.start, covr.end) {
		// data: a type indicator and a locale, 8 bytes, then the picture.
		if x.typ == "data" && x.end-x.start > 8 {
			pics = append(pics, picture{off: x.start + 8, size: x.end - x.start - 8, front: len(pics) == 0})
		}
	}
	return pics
}

// ── Ogg (Vorbis, Opus) ────────────────────────────────────────────────────

// oggPictures reassembles the comment header — the second packet of the
// first logical stream — from its pages and returns the pictures in its
// METADATA_BLOCK_PICTURE comments, base64-decoded. The packet is held in
// memory, so it is bounded: by inMemory, and by what a picture of most bytes
// takes in base64.
func oggPictures(b *budget, most int64) []picture {
	limit := min(int64(inMemory), most)*4/3 + 64<<10
	var pkt []byte
	var serial uint32
	packets := 0
	for off, pages := int64(0), 0; off+27 <= b.size && pages < 4096; pages++ {
		h := b.at(off, 27)
		if len(h) < 27 || string(h[:4]) != "OggS" {
			return nil
		}
		s := binary.LittleEndian.Uint32(h[14:])
		if pages == 0 {
			serial = s
		}
		segs := int(h[26])
		table := b.at(off+27, segs)
		if len(table) < segs {
			return nil
		}
		body := off + 27 + int64(segs)
		var total int64
		for _, l := range table {
			total += int64(l)
		}
		if s == serial {
			data := b.at(body, int(total))
			if int64(len(data)) < total {
				return nil
			}
			p := 0
			for _, l := range table {
				if packets == 1 {
					if pkt == nil && !b.toMemory() {
						return nil
					}
					if int64(len(pkt))+int64(l) > limit {
						return []picture{{size: tooLarge}}
					}
					pkt = append(pkt, data[p:p+int(l)]...)
				}
				p += int(l)
				if l < 255 { // the end of a packet
					packets++
					if packets == 2 {
						return commentPictures(pkt)
					}
				}
			}
		}
		off = body + total
	}
	return nil
}

// commentPictures reads a Vorbis or Opus comment header and decodes its
// METADATA_BLOCK_PICTURE comments.
func commentPictures(pkt []byte) []picture {
	switch {
	case bytes.HasPrefix(pkt, []byte("\x03vorbis")):
		pkt = pkt[7:]
	case bytes.HasPrefix(pkt, []byte("OpusTags")):
		pkt = pkt[8:]
	default:
		return nil
	}
	if len(pkt) < 8 {
		return nil
	}
	vendor := int64(binary.LittleEndian.Uint32(pkt))
	p := 4 + vendor
	if p+4 > int64(len(pkt)) {
		return nil
	}
	count := binary.LittleEndian.Uint32(pkt[p:])
	p += 4
	var pics []picture
	for k := uint32(0); k < count && k < 1024 && p+4 <= int64(len(pkt)); k++ {
		n := int64(binary.LittleEndian.Uint32(pkt[p:]))
		p += 4
		if p+n > int64(len(pkt)) {
			break
		}
		c := pkt[p : p+n]
		p += n
		const key = "METADATA_BLOCK_PICTURE="
		if len(c) <= len(key) || !strings.EqualFold(string(c[:len(key)]), key) {
			continue
		}
		block, err := base64.StdEncoding.DecodeString(string(c[len(key):]))
		if err != nil {
			continue
		}
		at := func(off int64, k int) []byte {
			if off < 0 || off >= int64(len(block)) {
				return nil
			}
			return block[off:min(off+int64(k), int64(len(block)))]
		}
		if x, ok := parsePictureBlock(at, int64(len(block)), 0); ok {
			x.data = block[x.off : x.off+x.size]
			pics = append(pics, x)
		}
	}
	return pics
}
