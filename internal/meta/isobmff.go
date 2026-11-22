package meta

import (
	"encoding/binary"
	"strings"
	"time"
)

// box is one ISO base media file box: its type, where its content starts
// and how long it is.
type box struct {
	typ        string
	start, end int64 // the content, after the header
}

// boxes lists the boxes between start and end of r, reading only their
// headers: an MP4's media data is passed over, not read.
func boxes(b *budget, start, end int64) []box {
	var out []box
	for off := start; off+8 <= end && len(out) < 4096; {
		h := b.at(off, 16)
		if len(h) < 8 {
			break
		}
		size := int64(binary.BigEndian.Uint32(h))
		typ := string(h[4:8])
		hdr := int64(8)
		switch size {
		case 0: // to the end
			size = end - off
		case 1: // a 64-bit size follows
			if len(h) < 16 {
				return out
			}
			size = int64(binary.BigEndian.Uint64(h[8:]))
			hdr = 16
		}
		if size < hdr || off+size > end {
			break
		}
		out = append(out, box{typ, off + hdr, off + size})
		off += size
	}
	return out
}

func find(bs []box, typ string) (box, bool) {
	for _, x := range bs {
		if x.typ == typ {
			return x, true
		}
	}
	return box{}, false
}

// content reads a box's content, up to max bytes.
func (x box) content(b *budget, max int) []byte {
	n := x.end - x.start
	if n > int64(max) {
		n = int64(max)
	}
	return b.at(x.start, int(n))
}

// readISOBMFF reads an MP4, a MOV, an M4A or a HEIC: the brand says which.
func readISOBMFF(b *budget) Info {
	top := boxes(b, 0, b.size)
	ftyp, _ := find(top, "ftyp")
	brands := ftyp.content(b, 256)
	major := ""
	if len(brands) >= 4 {
		major = string(brands[:4])
	}
	all := major
	for i := 8; i+4 <= len(brands); i += 4 {
		all += " " + string(brands[i:i+4])
	}
	if strings.Contains(all, "heic") || strings.Contains(all, "heix") || strings.Contains(all, "mif1") || strings.Contains(all, "avif") {
		return readHEIF(b, top, strings.Contains(all, "avif"))
	}
	info := Info{Kind: KindVideo, Format: "MP4"}
	switch {
	case major == "qt  ":
		info.Format = "QuickTime (MOV)"
	case major == "M4A " || major == "M4B " || major == "M4P ":
		info.Kind, info.Format = KindAudio, "M4A"
	case strings.HasPrefix(major, "3g"):
		info.Format = "3GP"
	}
	moov, ok := find(top, "moov")
	if !ok {
		return info
	}
	inner := boxes(b, moov.start, moov.end)
	if mvhd, ok := find(inner, "mvhd"); ok {
		info.Duration = mvhdDuration(mvhd.content(b, 120))
	}
	video := false
	for _, trak := range inner {
		if trak.typ != "trak" {
			continue
		}
		readTrak(b, trak, &info, &video)
	}
	if !video && info.Kind == KindVideo {
		info.Kind = KindAudio
		if info.Format == "MP4" {
			info.Format = "MP4 (audio)"
		}
	}
	if udta, ok := find(inner, "udta"); ok {
		readTags(b, udta, &info)
	}
	if info.Duration > 0 {
		info.Bitrate = int(float64(b.size) * 8 / info.Duration.Seconds())
	}
	return info
}

func mvhdDuration(c []byte) time.Duration {
	if len(c) < 20 {
		return 0
	}
	var scale, dur uint64
	if c[0] == 1 && len(c) >= 32 {
		scale, dur = uint64(binary.BigEndian.Uint32(c[20:])), binary.BigEndian.Uint64(c[24:])
	} else {
		scale, dur = uint64(binary.BigEndian.Uint32(c[12:])), uint64(binary.BigEndian.Uint32(c[16:]))
	}
	if scale == 0 || dur == 0 || dur == 0xFFFFFFFF || dur/scale > 1e7 {
		return 0
	}
	return time.Duration(float64(dur) / float64(scale) * float64(time.Second))
}

// readTrak takes a track's kind, its picture size and its codec.
func readTrak(b *budget, trak box, info *Info, video *bool) {
	tb := boxes(b, trak.start, trak.end)
	mdia, ok := find(tb, "mdia")
	if !ok {
		return
	}
	mb := boxes(b, mdia.start, mdia.end)
	handler := ""
	if hdlr, ok := find(mb, "hdlr"); ok {
		if c := hdlr.content(b, 24); len(c) >= 12 {
			handler = string(c[8:12])
		}
	}
	if handler != "vide" && handler != "soun" {
		return
	}
	codec := ""
	var stsdEntry []byte
	if minf, ok := find(mb, "minf"); ok {
		if stbl, ok := find(boxes(b, minf.start, minf.end), "stbl"); ok {
			if stsd, ok := find(boxes(b, stbl.start, stbl.end), "stsd"); ok {
				stsdEntry = stsd.content(b, 128)
				if len(stsdEntry) >= 16 {
					codec = codecName(string(stsdEntry[12:16]))
				}
			}
		}
	}
	if handler == "vide" && !*video {
		*video = true
		if tkhd, ok := find(tb, "tkhd"); ok {
			c := tkhd.content(b, 100)
			if n := len(c); n >= 84 {
				// The last eight bytes are width and height, 16.16 fixed point.
				info.Width = int(binary.BigEndian.Uint32(c[n-8:]) >> 16)
				info.Height = int(binary.BigEndian.Uint32(c[n-4:]) >> 16)
			}
		}
		if codec != "" {
			info.Codec = codec
		}
		return
	}
	if handler == "soun" {
		if len(stsdEntry) >= 44 {
			info.Channels = int(binary.BigEndian.Uint16(stsdEntry[32:]))
			info.SampleRate = int(binary.BigEndian.Uint32(stsdEntry[40:]) >> 16)
		}
		if codec != "" {
			if info.Codec == "" {
				info.Codec = codec
			} else if !strings.Contains(info.Codec, codec) {
				info.Codec += " · " + codec
			}
		}
	}
}

// codecName names a sample entry's four-character code.
func codecName(fourcc string) string {
	switch fourcc {
	case "avc1", "avc3":
		return "H.264"
	case "hvc1", "hev1":
		return "H.265 (HEVC)"
	case "av01":
		return "AV1"
	case "vp09":
		return "VP9"
	case "mp4v":
		return "MPEG-4 Visual"
	case "mp4a":
		return "AAC"
	case "alac":
		return "Apple Lossless"
	case "Opus":
		return "Opus"
	case "ac-3":
		return "AC-3"
	case "ec-3":
		return "E-AC-3"
	case "fLaC":
		return "FLAC"
	case "lpcm", "sowt", "twos", "in24", "in32", "fl32":
		return "PCM"
	case "apcn", "apch", "apcs", "apco", "ap4h":
		return "Apple ProRes"
	}
	return clean(strings.TrimSpace(fourcc))
}

// readTags reads iTunes-style tags: title, artist, album.
func readTags(b *budget, udta box, info *Info) {
	meta, ok := find(boxes(b, udta.start, udta.end), "meta")
	if !ok || meta.end-meta.start < 4 {
		return
	}
	// meta is a full box: four bytes of version and flags before its boxes.
	ilst, ok := find(boxes(b, meta.start+4, meta.end), "ilst")
	if !ok {
		return
	}
	for _, item := range boxes(b, ilst.start, ilst.end) {
		data, ok := find(boxes(b, item.start, item.end), "data")
		if !ok {
			continue
		}
		c := data.content(b, 1024)
		if len(c) < 8 {
			continue
		}
		v := clean(string(c[8:]))
		switch item.typ {
		case "\xa9nam":
			info.Title = v
		case "\xa9ART", "aART":
			if info.Author == "" {
				info.Author = v
			}
		case "\xa9alb":
			info.Album = v
		}
	}
}

// readHEIF reads a HEIC or AVIF still: the primary image's size from its
// ispe property, and its EXIF item through iinf and iloc — no decoding.
func readHEIF(b *budget, top []box, avif bool) Info {
	info := Info{Kind: KindImage, Format: "HEIC"}
	if avif {
		info.Format = "AVIF"
	}
	meta, ok := find(top, "meta")
	if !ok || meta.end-meta.start < 4 {
		return info
	}
	mb := boxes(b, meta.start+4, meta.end)
	if iprp, ok := find(mb, "iprp"); ok {
		if ipco, ok := find(boxes(b, iprp.start, iprp.end), "ipco"); ok {
			// The largest ispe is the full picture; the others are tiles or
			// thumbnails.
			for _, p := range boxes(b, ipco.start, ipco.end) {
				if p.typ != "ispe" {
					continue
				}
				if c := p.content(b, 12); len(c) >= 12 {
					w, h := int(binary.BigEndian.Uint32(c[4:])), int(binary.BigEndian.Uint32(c[8:]))
					if w*h > info.Width*info.Height {
						info.Width, info.Height = w, h
					}
				}
			}
		}
	}
	exifID, ok := heifExifItem(b, mb)
	if !ok {
		return info
	}
	off, length, ok := heifItemLocation(b, mb, exifID)
	if !ok || length < 8 || length > 1<<20 {
		return info
	}
	ex := b.at(off, int(length))
	if len(ex) < 8 {
		return info
	}
	// The EXIF item starts with the offset of the TIFF header within it.
	skip := int(binary.BigEndian.Uint32(ex)) + 4
	if skip < 4 || skip >= len(ex) {
		return info
	}
	w, h := info.Width, info.Height
	applyEXIF(&info, ex[skip:])
	if w > 0 {
		info.Width, info.Height = w, h
	}
	return info
}

// heifExifItem finds the ID of the item of type "Exif" in iinf.
func heifExifItem(b *budget, mb []box) (uint32, bool) {
	iinf, ok := find(mb, "iinf")
	if !ok {
		return 0, false
	}
	c := iinf.content(b, 1<<16)
	if len(c) < 6 {
		return 0, false
	}
	start := iinf.start + 6 // version, flags, a 16-bit count
	if c[0] != 0 {
		start = iinf.start + 8 // a 32-bit count
	}
	for _, infe := range boxes(b, start, iinf.end) {
		if infe.typ != "infe" {
			continue
		}
		e := infe.content(b, 64)
		if len(e) < 4 {
			continue
		}
		switch v := e[0]; {
		case v == 2 && len(e) >= 12:
			if string(e[8:12]) == "Exif" {
				return uint32(binary.BigEndian.Uint16(e[4:])), true
			}
		case v == 3 && len(e) >= 14:
			if string(e[10:14]) == "Exif" {
				return binary.BigEndian.Uint32(e[4:]), true
			}
		}
	}
	return 0, false
}

// heifItemLocation reads where an item's single extent lies, from iloc.
func heifItemLocation(b *budget, mb []box, id uint32) (int64, int64, bool) {
	iloc, ok := find(mb, "iloc")
	if !ok {
		return 0, 0, false
	}
	c := iloc.content(b, 1<<16)
	if len(c) < 8 {
		return 0, 0, false
	}
	version := c[0]
	offSize, lenSize := int(c[4]>>4), int(c[4]&15)
	baseSize, idxSize := int(c[5]>>4), 0
	if version == 1 || version == 2 {
		idxSize = int(c[5] & 15)
	}
	p := 6
	read := func(n int) (uint64, bool) {
		if n == 0 {
			return 0, true
		}
		if p+n > len(c) || (n != 2 && n != 4 && n != 8) {
			return 0, false
		}
		var v uint64
		for i := 0; i < n; i++ {
			v = v<<8 | uint64(c[p+i])
		}
		p += n
		return v, true
	}
	countSize := 2
	if version == 2 {
		countSize = 4
	}
	count, ok := read(countSize)
	if !ok {
		return 0, 0, false
	}
	for i := uint64(0); i < count && i < 4096; i++ {
		idSize := 2
		if version == 2 {
			idSize = 4
		}
		itemID, ok := read(idSize)
		if !ok {
			return 0, 0, false
		}
		method := uint64(0)
		if version == 1 || version == 2 {
			if method, ok = read(2); !ok {
				return 0, 0, false
			}
			method &= 15
		}
		if _, ok = read(2); !ok { // data reference index
			return 0, 0, false
		}
		base, ok := read(baseSize)
		if !ok {
			return 0, 0, false
		}
		extents, ok := read(2)
		if !ok {
			return 0, 0, false
		}
		var first [2]uint64
		for e := uint64(0); e < extents; e++ {
			if _, ok = read(idxSize); !ok {
				return 0, 0, false
			}
			o, ok1 := read(offSize)
			l, ok2 := read(lenSize)
			if !ok1 || !ok2 {
				return 0, 0, false
			}
			if e == 0 {
				first = [2]uint64{o, l}
			}
		}
		if uint32(itemID) == id && method == 0 && extents >= 1 {
			return int64(base + first[0]), int64(first[1]), true
		}
	}
	return 0, 0, false
}
