package meta

import (
	"bytes"
	"encoding/binary"
	"strings"
	"time"
	"unicode/utf16"
)

// readWAV reads the fmt chunk for the format, the data chunk's length for
// the duration, and LIST INFO for title and artist. Chunks are walked by
// their headers; the samples are not read.
func readWAV(b *budget, h []byte) Info {
	info := Info{Kind: KindAudio, Format: "WAV"}
	var byteRate, dataLen int64
	for off, chunks := int64(12), 0; off+8 <= b.size && chunks < 256; chunks++ {
		var c []byte
		if off+8 <= int64(len(h)) {
			c = h[off : off+8]
		} else if c = b.at(off, 8); len(c) < 8 {
			break
		}
		typ := string(c[:4])
		n := int64(binary.LittleEndian.Uint32(c[4:]))
		body := off + 8
		switch typ {
		case "fmt ":
			f := b.at(body, 16)
			if len(f) >= 16 {
				info.Codec = wavCodec(binary.LittleEndian.Uint16(f))
				info.Channels = int(binary.LittleEndian.Uint16(f[2:]))
				info.SampleRate = int(binary.LittleEndian.Uint32(f[4:]))
				byteRate = int64(binary.LittleEndian.Uint32(f[8:]))
				info.Bitrate = int(byteRate * 8)
			}
		case "data":
			dataLen = n
			if dataLen > b.size-body {
				dataLen = b.size - body
			}
		case "LIST":
			if l := b.at(body, int(min(n, 4096))); len(l) >= 4 && string(l[:4]) == "INFO" {
				riffInfo(l[4:], &info)
			}
		}
		if n < 0 {
			break
		}
		off = body + n + n%2
	}
	if byteRate > 0 && dataLen > 0 {
		info.Duration = time.Duration(float64(dataLen) / float64(byteRate) * float64(time.Second))
	}
	return info
}

func wavCodec(tag uint16) string {
	switch tag {
	case 1, 0xFFFE:
		return "PCM"
	case 3:
		return "PCM (float)"
	case 6:
		return "A-law"
	case 7:
		return "µ-law"
	case 0x55:
		return "MP3"
	}
	return ""
}

func riffInfo(b []byte, info *Info) {
	for i := 0; i+8 <= len(b); {
		id := string(b[i : i+4])
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		if n < 0 || i+8+n > len(b) {
			return
		}
		v := clean(strings.TrimRight(string(b[i+8:i+8+n]), "\x00"))
		switch id {
		case "INAM":
			info.Title = v
		case "IART":
			info.Author = v
		case "IPRD":
			info.Album = v
		}
		i += 8 + n + n%2
	}
}

// readFLAC reads STREAMINFO and the Vorbis comment block.
func readFLAC(b []byte) Info {
	info := Info{Kind: KindAudio, Format: "FLAC", Codec: "FLAC"}
	for i := 4; i+4 <= len(b); {
		last := b[i]&0x80 != 0
		typ := b[i] & 0x7F
		n := int(b[i+1])<<16 | int(b[i+2])<<8 | int(b[i+3])
		if i+4+n > len(b) {
			break
		}
		d := b[i+4 : i+4+n]
		switch typ {
		case 0: // STREAMINFO
			if len(d) >= 18 {
				rate := int(d[10])<<12 | int(d[11])<<4 | int(d[12])>>4
				info.SampleRate = rate
				info.Channels = int(d[12]>>1&7) + 1
				samples := uint64(d[13]&15)<<32 | uint64(binary.BigEndian.Uint32(d[14:]))
				if rate > 0 && samples > 0 {
					info.Duration = time.Duration(float64(samples) / float64(rate) * float64(time.Second))
				}
			}
		case 4: // VORBIS_COMMENT
			vorbisComments(d, &info)
		}
		if last {
			break
		}
		i += 4 + n
	}
	return info
}

func vorbisComments(d []byte, info *Info) {
	if len(d) < 8 {
		return
	}
	vendor := int(binary.LittleEndian.Uint32(d))
	p := 4 + vendor
	if p+4 > len(d) || vendor < 0 {
		return
	}
	count := int(binary.LittleEndian.Uint32(d[p:]))
	p += 4
	for k := 0; k < count && k < 1024 && p+4 <= len(d); k++ {
		n := int(binary.LittleEndian.Uint32(d[p:]))
		p += 4
		if n < 0 || p+n > len(d) {
			return
		}
		key, val, ok := strings.Cut(string(d[p:p+n]), "=")
		p += n
		if !ok {
			continue
		}
		switch strings.ToUpper(key) {
		case "TITLE":
			info.Title = clean(val)
		case "ARTIST":
			if info.Author == "" {
				info.Author = clean(val)
			}
		case "ALBUM":
			info.Album = clean(val)
		}
	}
}

// mpeg bitrates (kbit/s) for MPEG-1 Layer III and MPEG-2/2.5 Layer III,
// and sample rates by version.
var (
	mp3Rates1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mp3Rates2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	mp3Hz     = map[int][3]int{3: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 0: {11025, 12000, 8000}}
)

func isMPEGFrame(b []byte) bool {
	return len(b) >= 4 && b[0] == 0xFF && b[1]&0xE6 == 0xE2 && b[2]&0xF0 != 0xF0 && b[2]&0x0C != 0x0C
}

// readMP3 reads ID3v2 tags, then the first frame's header and, when there
// is one, its Xing or Info header for an exact duration; otherwise the
// duration of a constant bitrate.
func readMP3(b *budget, h []byte) Info {
	info := Info{Kind: KindAudio, Format: "MP3", Codec: "MP3"}
	start := 0
	if bytes.HasPrefix(h, []byte("ID3")) && len(h) >= 10 {
		size := int(h[6]&0x7F)<<21 | int(h[7]&0x7F)<<14 | int(h[8]&0x7F)<<7 | int(h[9]&0x7F)
		end := 10 + size
		if end > len(h) {
			end = len(h)
		}
		id3Frames(h[10:end], h[3], &info)
		start = 10 + size
		if h[5]&0x10 != 0 {
			start += 10 // a footer
		}
	}
	// The first frame: right after the tag, or a little later after padding.
	var f []byte
	for i := start; i < start+4096 && i+4 <= len(h); i++ {
		if isMPEGFrame(h[i:]) {
			f = h[i:]
			start = i
			break
		}
	}
	if f == nil {
		return info
	}
	version := int(f[1]>>3) & 3 // 3: MPEG-1, 2: MPEG-2, 0: MPEG-2.5
	rateIdx, hzIdx := int(f[2]>>4), int(f[2]>>2)&3
	kbps := mp3Rates2[rateIdx]
	if version == 3 {
		kbps = mp3Rates1[rateIdx]
	}
	hz := 0
	if r, ok := mp3Hz[version]; ok && hzIdx < 3 {
		hz = r[hzIdx]
	}
	mono := f[3]>>6 == 3
	info.SampleRate = hz
	info.Channels = 2
	if mono {
		info.Channels = 1
	}
	// The Xing/Info header sits after the side information.
	side := 32
	switch {
	case version == 3 && mono:
		side = 17
	case version != 3 && !mono:
		side = 17
	case version != 3 && mono:
		side = 9
	}
	if x := 4 + side; x+12 <= len(f) && (string(f[x:x+4]) == "Xing" || string(f[x:x+4]) == "Info") {
		flags := binary.BigEndian.Uint32(f[x+4:])
		if flags&1 != 0 && hz > 0 {
			frames := binary.BigEndian.Uint32(f[x+8:])
			perFrame := 1152
			if version != 3 {
				perFrame = 576
			}
			info.Duration = time.Duration(float64(frames) * float64(perFrame) / float64(hz) * float64(time.Second))
		}
	}
	audio := b.size - int64(start)
	if info.Duration > 0 {
		info.Bitrate = int(float64(audio) * 8 / info.Duration.Seconds())
	} else if kbps > 0 {
		info.Bitrate = kbps * 1000
		info.Duration = time.Duration(float64(audio) * 8 / float64(info.Bitrate) * float64(time.Second))
	}
	return info
}

// id3Frames reads TIT2, TPE1 and TALB from an ID3v2.3 or 2.4 tag.
func id3Frames(b []byte, major byte, info *Info) {
	if major < 3 {
		return
	}
	for i := 0; i+10 <= len(b); {
		id := string(b[i : i+4])
		if id[0] == 0 {
			return // padding
		}
		var n int
		if major == 4 {
			n = int(b[i+4]&0x7F)<<21 | int(b[i+5]&0x7F)<<14 | int(b[i+6]&0x7F)<<7 | int(b[i+7]&0x7F)
		} else {
			n = int(binary.BigEndian.Uint32(b[i+4:]))
		}
		if n <= 0 || i+10+n > len(b) {
			return
		}
		data := b[i+10 : i+10+n]
		switch id {
		case "TIT2":
			info.Title = id3Text(data)
		case "TPE1":
			info.Author = id3Text(data)
		case "TALB":
			info.Album = id3Text(data)
		}
		i += 10 + n
	}
}

// id3Text decodes a text frame in any of ID3's four encodings.
func id3Text(d []byte) string {
	if len(d) < 1 {
		return ""
	}
	enc, s := d[0], d[1:]
	switch enc {
	case 0: // ISO-8859-1
		r := make([]rune, len(s))
		for i, c := range s {
			r[i] = rune(c)
		}
		return clean(strings.TrimRight(string(r), "\x00"))
	case 1, 2: // UTF-16 with a BOM, UTF-16BE
		big := enc == 2
		if len(s) >= 2 && s[0] == 0xFF && s[1] == 0xFE {
			big, s = false, s[2:]
		} else if len(s) >= 2 && s[0] == 0xFE && s[1] == 0xFF {
			big, s = true, s[2:]
		}
		u := make([]uint16, 0, len(s)/2)
		for i := 0; i+1 < len(s); i += 2 {
			if big {
				u = append(u, uint16(s[i])<<8|uint16(s[i+1]))
			} else {
				u = append(u, uint16(s[i])|uint16(s[i+1])<<8)
			}
		}
		return clean(strings.TrimRight(string(utf16.Decode(u)), "\x00"))
	default: // UTF-8
		return clean(strings.TrimRight(string(s), "\x00"))
	}
}
