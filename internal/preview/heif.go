package preview

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// A HEIF file — what a phone writes as .heic — is a box structure whose
// meta box lists items (the coded image, often a grid of tiles; a small
// thumbnail; Exif), their properties and where their bytes are. A thumbnail
// of 320 pixels needs only the thumbnail item: heifThumbnail takes it out
// as a file of its own, with nothing else in it, which libheif decodes in
// a few milliseconds instead of the seconds a 12-megapixel grid takes.

// heifLimits bound what a file may make the parser read or keep.
const (
	heifMetaMax  = 4 << 20 // the meta box: item lists, properties, idat
	heifItemsMax = 4096
)

var errHEIF = errors.New("preview: not a HEIF file ferry can read")

// heifItem is one item: its type, its properties (as indexes into
// heifFile.props, 1-based as in ipma, with the essential bit), and where
// its bytes are.
type heifItem struct {
	id      uint32
	typ     string
	props   []heifAssoc
	extents []heifExtent
	idat    bool // the extents are in the meta box's idat, not the file
}

type heifAssoc struct {
	index     int
	essential bool
}

type heifExtent struct{ off, len int64 }

type heifRef struct {
	typ  string
	from uint32
	to   []uint32
}

// heifFile is what heifThumbnail and the tests need of a file.
type heifFile struct {
	primary uint32
	items   []*heifItem
	refs    []heifRef
	props   [][]byte // whole property boxes, header included, in ipco order
	idat    []byte
}

func (f *heifFile) item(id uint32) *heifItem {
	for _, it := range f.items {
		if it.id == id {
			return it
		}
	}
	return nil
}

// thumbnail is the item that is the primary image's thumbnail, coded as one
// HEVC image (a thumbnail that is itself a grid is left alone).
func (f *heifFile) thumbnail() *heifItem {
	for _, r := range f.refs {
		if r.typ != "thmb" {
			continue
		}
		for _, to := range r.to {
			if to == f.primary {
				if it := f.item(r.from); it != nil && it.typ == "hvc1" {
					return it
				}
			}
		}
	}
	return nil
}

// data reads an item's bytes, at most max of them, from the file r of size
// bytes. Every extent is checked against the bounds before anything is
// allocated: the lengths come from the file, and their sum must not be
// able to wrap round into a small number that passes.
func (f *heifFile) data(r io.ReaderAt, it *heifItem, size, max int64) ([]byte, error) {
	if len(it.extents) == 0 || len(it.extents) > 1024 {
		return nil, errHEIF
	}
	var total int64
	for _, e := range it.extents {
		if e.len <= 0 || e.len > max-total || e.off < 0 {
			return nil, errHEIF
		}
		limit := size
		if it.idat {
			limit = int64(len(f.idat))
		}
		if e.len > limit || e.off > limit-e.len {
			return nil, errHEIF
		}
		total += e.len
	}
	out := make([]byte, total)
	p := int64(0)
	for _, e := range it.extents {
		dst := out[p : p+e.len]
		if it.idat {
			copy(dst, f.idat[e.off:e.off+e.len])
		} else if _, err := r.ReadAt(dst, e.off); err != nil {
			return nil, err
		}
		p += e.len
	}
	return out, nil
}

// ── Reading ───────────────────────────────────────────────────────────────

// reader walks bytes, remembering the first read past the end.
type reader struct {
	b   []byte
	p   int
	bad bool
}

func (r *reader) take(n int) []byte {
	if n < 0 || r.p+n > len(r.b) {
		r.bad = true
		r.p = len(r.b)
		return make([]byte, max(n, 0))
	}
	out := r.b[r.p : r.p+n]
	r.p += n
	return out
}

func (r *reader) u8() uint8   { return r.take(1)[0] }
func (r *reader) u16() uint16 { return binary.BigEndian.Uint16(r.take(2)) }
func (r *reader) u32() uint32 { return binary.BigEndian.Uint32(r.take(4)) }

// uint reads an unsigned integer of n bytes (0, 4 or 8, as iloc sizes are).
func (r *reader) uint(n int) int64 {
	switch n {
	case 0:
		return 0
	case 4:
		return int64(r.u32())
	case 8:
		v := binary.BigEndian.Uint64(r.take(8))
		if v > 1<<62 {
			r.bad = true
		}
		return int64(v)
	}
	r.bad = true
	return 0
}

// children lists the boxes in b: type, body and the whole box.
func children(b []byte) (out []struct {
	typ        string
	body, full []byte
}) {
	for p := 0; p+8 <= len(b) && len(out) < 1024; {
		n := int(binary.BigEndian.Uint32(b[p:]))
		typ := string(b[p+4 : p+8])
		hdr := 8
		switch n {
		case 1:
			if p+16 > len(b) {
				return out
			}
			v := binary.BigEndian.Uint64(b[p+8:])
			if v > uint64(len(b)) {
				return out
			}
			n, hdr = int(v), 16
		case 0:
			n = len(b) - p
		}
		if n < hdr || p+n > len(b) {
			return out
		}
		out = append(out, struct {
			typ        string
			body, full []byte
		}{typ, b[p+hdr : p+n], b[p : p+n]})
		p += n
	}
	return out
}

// parseHEIF reads the meta box of the file r of size bytes.
func parseHEIF(r io.ReaderAt, size int64) (*heifFile, error) {
	// The top-level boxes, by their headers: ftyp, meta, mdat…
	var meta []byte
	for off, n := int64(0), 0; off+8 <= size && n < 64; n++ {
		h := make([]byte, 16)
		m, _ := r.ReadAt(h, off)
		if m < 8 {
			break
		}
		bs := int64(binary.BigEndian.Uint32(h))
		hdr := int64(8)
		switch bs {
		case 1:
			if m < 16 {
				return nil, errHEIF
			}
			bs, hdr = int64(binary.BigEndian.Uint64(h[8:])), 16
		case 0:
			bs = size - off
		}
		if bs < hdr || bs > size-off {
			return nil, errHEIF
		}
		if string(h[4:8]) == "meta" {
			if bs-hdr > heifMetaMax {
				return nil, errHEIF
			}
			meta = make([]byte, bs-hdr)
			if _, err := r.ReadAt(meta, off+hdr); err != nil {
				return nil, err
			}
			break
		}
		off += bs
	}
	if len(meta) < 4 {
		return nil, errHEIF
	}
	f := &heifFile{}
	for _, c := range children(meta[4:]) { // meta is a full box
		rd := &reader{b: c.body}
		switch c.typ {
		case "pitm":
			if rd.u8() == 0 {
				rd.take(3)
				f.primary = uint32(rd.u16())
			} else {
				rd.take(3)
				f.primary = rd.u32()
			}
		case "iinf":
			parseIinf(f, rd)
		case "iref":
			parseIref(f, rd)
		case "iprp":
			for _, p := range children(c.body) {
				switch p.typ {
				case "ipco":
					for _, prop := range children(p.body) {
						f.props = append(f.props, prop.full)
					}
				case "ipma":
					parseIpma(f, &reader{b: p.body})
				}
			}
		case "iloc":
			parseIloc(f, rd)
		case "idat":
			f.idat = c.body
		}
		if rd.bad {
			return nil, errHEIF
		}
	}
	if f.primary == 0 || f.item(f.primary) == nil {
		return nil, errHEIF
	}
	return f, nil
}

func parseIinf(f *heifFile, rd *reader) {
	v := rd.u8()
	rd.take(3)
	var n int
	if v == 0 {
		n = int(rd.u16())
	} else {
		n = int(rd.u32())
	}
	if n > heifItemsMax {
		rd.bad = true
		return
	}
	for _, c := range children(rd.b[rd.p:]) {
		if c.typ != "infe" {
			continue
		}
		e := &reader{b: c.body}
		ev := e.u8()
		e.take(3)
		if ev < 2 {
			continue // without an item type
		}
		var id uint32
		if ev == 2 {
			id = uint32(e.u16())
		} else {
			id = e.u32()
		}
		e.take(2) // protection index
		typ := string(e.take(4))
		if !e.bad && f.item(id) == nil && len(f.items) < heifItemsMax {
			f.items = append(f.items, &heifItem{id: id, typ: typ})
		}
	}
}

func parseIref(f *heifFile, rd *reader) {
	v := rd.u8()
	rd.take(3)
	for _, c := range children(rd.b[rd.p:]) {
		e := &reader{b: c.body}
		id := func() uint32 {
			if v == 0 {
				return uint32(e.u16())
			}
			return e.u32()
		}
		ref := heifRef{typ: c.typ, from: id()}
		n := int(e.u16())
		for i := 0; i < n && !e.bad; i++ {
			ref.to = append(ref.to, id())
		}
		if !e.bad {
			f.refs = append(f.refs, ref)
		}
	}
}

func parseIpma(f *heifFile, rd *reader) {
	v := rd.u8()
	flags := rd.take(3)
	wide := flags[2]&1 != 0
	n := int(rd.u32())
	for i := 0; i < n && !rd.bad && i < heifItemsMax; i++ {
		var id uint32
		if v < 1 {
			id = uint32(rd.u16())
		} else {
			id = rd.u32()
		}
		k := int(rd.u8())
		var as []heifAssoc
		for j := 0; j < k; j++ {
			if wide {
				x := rd.u16()
				as = append(as, heifAssoc{int(x & 0x7FFF), x&0x8000 != 0})
			} else {
				x := rd.u8()
				as = append(as, heifAssoc{int(x & 0x7F), x&0x80 != 0})
			}
		}
		if it := f.item(id); it != nil {
			it.props = as
		}
	}
}

func parseIloc(f *heifFile, rd *reader) {
	v := rd.u8()
	rd.take(3)
	b := rd.u8()
	offSize, lenSize := int(b>>4), int(b&15)
	b = rd.u8()
	baseSize, idxSize := int(b>>4), int(b&15)
	if v == 0 {
		idxSize = 0
	}
	var n int
	if v < 2 {
		n = int(rd.u16())
	} else {
		n = int(rd.u32())
	}
	for i := 0; i < n && !rd.bad && i < heifItemsMax; i++ {
		var id uint32
		if v < 2 {
			id = uint32(rd.u16())
		} else {
			id = rd.u32()
		}
		method := 0
		if v >= 1 {
			method = int(rd.u16() & 15)
		}
		rd.take(2) // data reference index
		base := rd.uint(baseSize)
		k := int(rd.u16())
		var ext []heifExtent
		for j := 0; j < k && !rd.bad; j++ {
			if idxSize > 0 {
				rd.uint(idxSize)
			}
			off := rd.uint(offSize)
			l := rd.uint(lenSize)
			if off > math.MaxInt64-base {
				rd.bad = true
				break
			}
			ext = append(ext, heifExtent{base + off, l})
		}
		if it := f.item(id); it != nil && (method == 0 || method == 1) {
			it.extents, it.idat = ext, method == 1
		}
	}
}

// ── Writing ───────────────────────────────────────────────────────────────

// box writes a box of type typ around the body.
func box(typ string, body ...[]byte) []byte {
	b := bytes.Join(body, nil)
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(b)))
	return append(append(out, typ...), b...)
}

// full is a full box's version and flags.
func full(v uint8, flags uint32) []byte {
	return []byte{v, byte(flags >> 16), byte(flags >> 8), byte(flags)}
}

func be16(n int) []byte    { return binary.BigEndian.AppendUint16(nil, uint16(n)) }
func be32(n uint32) []byte { return binary.BigEndian.AppendUint32(nil, n) }

// writeHEIF writes items (their bytes in data, by id) as a file of its own:
// the meta box with every item in the file's mdat.
func writeHEIF(f *heifFile, data map[uint32][]byte) []byte {
	ftyp := box("ftyp", []byte("heic"), be32(0), []byte("mif1heic"))
	hdlr := box("hdlr", full(0, 0), be32(0), []byte("pict"), make([]byte, 12), []byte{0})
	dinf := box("dinf", box("dref", full(0, 0), be32(1), box("url ", full(0, 1))))
	pitm := box("pitm", full(0, 0), be16(int(f.primary)))
	var infes [][]byte
	for _, it := range f.items {
		infes = append(infes, box("infe", full(2, 0), be16(int(it.id)), be16(0), []byte(it.typ), []byte{0}))
	}
	iinf := box("iinf", append([][]byte{full(0, 0), be16(len(f.items))}, infes...)...)
	var irefs [][]byte
	for _, r := range f.refs {
		e := [][]byte{be16(int(r.from)), be16(len(r.to))}
		for _, to := range r.to {
			e = append(e, be16(int(to)))
		}
		irefs = append(irefs, box(r.typ, e...))
	}
	var iref []byte
	if len(irefs) > 0 {
		iref = box("iref", append([][]byte{full(0, 0)}, irefs...)...)
	}
	var ipma [][]byte
	for _, it := range f.items {
		e := []byte{byte(it.id >> 8), byte(it.id), byte(len(it.props))}
		for _, a := range it.props {
			x := byte(a.index & 0x7F)
			if a.essential {
				x |= 0x80
			}
			e = append(e, x)
		}
		ipma = append(ipma, e)
	}
	iprp := box("iprp", box("ipco", f.props...), box("ipma", append([][]byte{full(0, 0), be32(uint32(len(f.items)))}, ipma...)...))

	// iloc points into mdat, whose place depends on the size of meta, which
	// includes iloc: its size does not depend on the offsets, so it is
	// written once to measure and once for real.
	iloc := func(mdatBody int) []byte {
		e := [][]byte{full(0, 0), {0x44, 0x00}, be16(len(f.items))}
		off := mdatBody
		for _, it := range f.items {
			d := data[it.id]
			e = append(e, be16(int(it.id)), be16(0), be16(1), be32(uint32(off)), be32(uint32(len(d))))
			off += len(d)
		}
		return box("iloc", e...)
	}
	meta := func(mdatBody int) []byte {
		return box("meta", full(0, 0), hdlr, dinf, pitm, iinf, iref, iprp, iloc(mdatBody))
	}
	start := len(ftyp) + len(meta(0)) + 8
	var mdat [][]byte
	for _, it := range f.items {
		mdat = append(mdat, data[it.id])
	}
	return bytes.Join([][]byte{ftyp, meta(start), box("mdat", mdat...)}, nil)
}

// heifThumbnail returns the thumbnail item of the HEIF file r as a file of
// its own, or nil when it has none ferry can use. Its properties — the
// decoder's configuration, the size, a rotation — go with it.
func heifThumbnail(r io.ReaderAt, size, max int64) ([]byte, error) {
	f, err := parseHEIF(r, size)
	if err != nil {
		return nil, err
	}
	th := f.thumbnail()
	if th == nil {
		return nil, nil
	}
	d, err := f.data(r, th, size, max)
	if err != nil {
		return nil, err
	}
	out := &heifFile{primary: 1, items: []*heifItem{{id: 1, typ: "hvc1"}}}
	for _, a := range th.props {
		if a.index < 1 || a.index > len(f.props) {
			return nil, errHEIF
		}
		out.props = append(out.props, f.props[a.index-1])
		out.items[0].props = append(out.items[0].props, heifAssoc{len(out.props), a.essential})
	}
	if len(out.props) > 127 {
		return nil, errHEIF
	}
	return writeHEIF(out, map[uint32][]byte{1: d}), nil
}
