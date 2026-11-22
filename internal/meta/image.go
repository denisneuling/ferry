package meta

import (
	"encoding/binary"
)

// readJPEG walks a JPEG's segments up to the image data: the frame header
// for the size, APP1 for EXIF.
func readJPEG(b []byte) Info {
	info := Info{Kind: KindImage, Format: "JPEG"}
	var exif []byte
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			break
		}
		marker := b[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 {
			i += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			break
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			break
		}
		seg := b[i+4 : i+2+n]
		switch {
		case marker == 0xE1 && exif == nil && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00":
			exif = seg[6:]
		case marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC && len(seg) >= 5:
			info.Height = int(binary.BigEndian.Uint16(seg[1:]))
			info.Width = int(binary.BigEndian.Uint16(seg[3:]))
		}
		i += 2 + n
	}
	if exif != nil {
		w, h := info.Width, info.Height
		applyEXIF(&info, exif)
		// The frame header is the image; EXIF's pixel dimensions may describe
		// the original a program resized from.
		if w > 0 {
			info.Width, info.Height = w, h
		}
	}
	return info
}

// readPNG reads IHDR for the size and an eXIf chunk, if any.
func readPNG(b []byte) Info {
	info := Info{Kind: KindImage, Format: "PNG"}
	for i := 8; i+8 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[i:]))
		typ := string(b[i+4 : i+8])
		if n < 0 || i+8+n > len(b) {
			break
		}
		data := b[i+8 : i+8+n]
		switch typ {
		case "IHDR":
			if len(data) >= 8 {
				info.Width = int(binary.BigEndian.Uint32(data))
				info.Height = int(binary.BigEndian.Uint32(data[4:]))
			}
		case "eXIf":
			w, h := info.Width, info.Height
			applyEXIF(&info, data)
			info.Width, info.Height = w, h
		case "IDAT", "IEND":
			return info
		}
		i += 12 + n
	}
	return info
}

func readGIF(b []byte) Info {
	info := Info{Kind: KindImage, Format: "GIF"}
	if len(b) >= 10 {
		info.Width = int(binary.LittleEndian.Uint16(b[6:]))
		info.Height = int(binary.LittleEndian.Uint16(b[8:]))
	}
	return info
}

func readBMP(b []byte) Info {
	info := Info{Kind: KindImage, Format: "BMP"}
	w := int(int32(binary.LittleEndian.Uint32(b[18:])))
	h := int(int32(binary.LittleEndian.Uint32(b[22:])))
	if h < 0 {
		h = -h // top-down
	}
	if w > 0 && h > 0 {
		info.Width, info.Height = w, h
	}
	return info
}

// readWebP reads the size from VP8, VP8L or VP8X, and an EXIF chunk.
func readWebP(b []byte) Info {
	info := Info{Kind: KindImage, Format: "WebP"}
	for i := 12; i+8 <= len(b); {
		typ := string(b[i : i+4])
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		if n < 0 || i+8+n > len(b) {
			break
		}
		data := b[i+8 : i+8+n]
		switch typ {
		case "VP8X":
			if len(data) >= 10 {
				// 24-bit sizes, stored minus one.
				info.Width = 1 + (int(data[4]) | int(data[5])<<8 | int(data[6])<<16)
				info.Height = 1 + (int(data[7]) | int(data[8])<<8 | int(data[9])<<16)
			}
		case "VP8 ":
			if info.Width == 0 && len(data) >= 10 && data[3] == 0x9d && data[4] == 0x01 && data[5] == 0x2a {
				info.Width = int(binary.LittleEndian.Uint16(data[6:]) & 0x3fff)
				info.Height = int(binary.LittleEndian.Uint16(data[8:]) & 0x3fff)
			}
		case "VP8L":
			if info.Width == 0 && len(data) >= 5 && data[0] == 0x2f {
				v := binary.LittleEndian.Uint32(data[1:])
				info.Width = int(v&0x3fff) + 1
				info.Height = int((v>>14)&0x3fff) + 1
			}
		case "EXIF":
			w, h := info.Width, info.Height
			ex := data
			if len(ex) > 6 && string(ex[:6]) == "Exif\x00\x00" {
				ex = ex[6:]
			}
			applyEXIF(&info, ex)
			info.Width, info.Height = w, h
		}
		i += 8 + n + n%2
	}
	return info
}
