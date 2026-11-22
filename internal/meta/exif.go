package meta

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// tiff reads a TIFF structure — the body of an EXIF block — bounding every
// offset by the block: the bytes come from whoever made the file.
type tiff struct {
	b  []byte
	bo binary.ByteOrder
}

func newTIFF(b []byte) *tiff {
	if len(b) < 8 {
		return nil
	}
	switch string(b[:4]) {
	case "II*\x00":
		return &tiff{b, binary.LittleEndian}
	case "MM\x00*":
		return &tiff{b, binary.BigEndian}
	}
	return nil
}

// entry is one tag of an IFD.
type entry struct {
	typ, count uint32
	val        []byte // the value's bytes, inline or at their offset
}

// ifd reads the directory at off into its tags. At most 512 entries are
// read, whatever the count claims.
func (t *tiff) ifd(off uint32) map[uint16]entry {
	if int64(off)+2 > int64(len(t.b)) || off < 8 {
		return nil
	}
	n := int(t.bo.Uint16(t.b[off:]))
	out := map[uint16]entry{}
	for k := 0; k < n && k < 512; k++ {
		e := int(off) + 2 + 12*k
		if e+12 > len(t.b) {
			break
		}
		tag := t.bo.Uint16(t.b[e:])
		typ := uint32(t.bo.Uint16(t.b[e+2:]))
		count := t.bo.Uint32(t.b[e+4:])
		size := typeSize(typ)
		if size == 0 || count > 1<<20 {
			continue
		}
		total := int64(size) * int64(count)
		var val []byte
		if total <= 4 {
			val = t.b[e+8 : e+8+int(total)]
		} else {
			o := int64(t.bo.Uint32(t.b[e+8:]))
			if o+total > int64(len(t.b)) {
				continue
			}
			val = t.b[o : o+total]
		}
		out[tag] = entry{typ, count, val}
	}
	return out
}

func typeSize(typ uint32) int {
	switch typ {
	case 1, 2, 6, 7: // BYTE, ASCII, SBYTE, UNDEFINED
		return 1
	case 3, 8: // SHORT, SSHORT
		return 2
	case 4, 9, 11: // LONG, SLONG, FLOAT
		return 4
	case 5, 10, 12: // RATIONAL, SRATIONAL, DOUBLE
		return 8
	}
	return 0
}

func (t *tiff) str(e entry) string {
	if e.typ != 2 && e.typ != 7 {
		return ""
	}
	return clean(strings.TrimRight(string(e.val), "\x00 "))
}

func (t *tiff) uint(e entry) (uint32, bool) {
	switch e.typ {
	case 3:
		if len(e.val) >= 2 {
			return uint32(t.bo.Uint16(e.val)), true
		}
	case 4:
		if len(e.val) >= 4 {
			return t.bo.Uint32(e.val), true
		}
	}
	return 0, false
}

// rational reads the i-th rational of an entry as numerator and denominator.
func (t *tiff) rational(e entry, i int) (num, den float64, ok bool) {
	if (e.typ != 5 && e.typ != 10) || len(e.val) < 8*(i+1) {
		return 0, 0, false
	}
	p := e.val[8*i:]
	if e.typ == 10 {
		num, den = float64(int32(t.bo.Uint32(p))), float64(int32(t.bo.Uint32(p[4:])))
	} else {
		num, den = float64(t.bo.Uint32(p)), float64(t.bo.Uint32(p[4:]))
	}
	return num, den, den != 0
}

func (t *tiff) float(e entry) (float64, bool) {
	n, d, ok := t.rational(e, 0)
	if !ok {
		return 0, false
	}
	return n / d, true
}

// EXIF tags ferry shows.
const (
	tagMake        = 0x010F
	tagModel       = 0x0110
	tagOrientation = 0x0112
	tagDateTime    = 0x0132
	tagExifIFD     = 0x8769
	tagGPSIFD      = 0x8825
	tagExposure    = 0x829A
	tagFNumber     = 0x829D
	tagISO         = 0x8827
	tagTaken       = 0x9003
	tagTakenOffset = 0x9011
	tagFocal       = 0x920A
	tagPixelX      = 0xA002
	tagPixelY      = 0xA003
	tagLensMake    = 0xA433
	tagLensModel   = 0xA434
	tagWidth       = 0x0100
	tagHeight      = 0x0101
)

// applyEXIF fills info from an EXIF body (a TIFF structure); what the
// file's own headers said — the dimensions — stays when EXIF has none.
func applyEXIF(info *Info, b []byte) {
	t := newTIFF(b)
	if t == nil {
		return
	}
	ifd0 := t.ifd(t.bo.Uint32(b[4:]))
	if ifd0 == nil {
		return
	}
	mk, model := t.str(ifd0[tagMake]), t.str(ifd0[tagModel])
	switch {
	case mk != "" && model != "" && !strings.HasPrefix(strings.ToLower(model), strings.ToLower(strings.Fields(mk)[0])):
		info.Camera = mk + " " + model
	case model != "":
		info.Camera = model
	default:
		info.Camera = mk
	}
	if o, ok := t.uint(ifd0[tagOrientation]); ok && o >= 1 && o <= 8 {
		info.Orientation = int(o)
	}
	if info.Format == "TIFF" {
		if w, ok := t.uint(ifd0[tagWidth]); ok {
			info.Width = int(w)
		}
		if h, ok := t.uint(ifd0[tagHeight]); ok {
			info.Height = int(h)
		}
	}
	taken := t.str(ifd0[tagDateTime])
	if e, ok := ifd0[tagExifIFD]; ok {
		if off, ok := t.uint(e); ok {
			ex := t.ifd(off)
			if s := t.str(ex[tagTaken]); s != "" {
				taken = s
			}
			if taken != "" {
				info.Taken = exifTime(taken, t.str(ex[tagTakenOffset]))
			}
			if f, ok := t.float(ex[tagFNumber]); ok && f > 0 && f < 1000 {
				info.FNumber = math.Round(f*10) / 10
			}
			if n, d, ok := t.rational(ex[tagExposure], 0); ok && n > 0 {
				info.Exposure = exposure(n, d)
			}
			if iso, ok := t.uint(ex[tagISO]); ok && iso > 0 {
				info.ISO = int(iso)
			}
			if f, ok := t.float(ex[tagFocal]); ok && f > 0 && f < 100000 {
				info.FocalLength = math.Round(f*10) / 10
			}
			lens := t.str(ex[tagLensModel])
			if lm := t.str(ex[tagLensMake]); lm != "" && lens != "" && !strings.HasPrefix(lens, lm) {
				lens = lm + " " + lens
			}
			info.Lens = lens
			if info.Width == 0 {
				if w, ok := t.uint(ex[tagPixelX]); ok {
					info.Width = int(w)
				}
				if h, ok := t.uint(ex[tagPixelY]); ok {
					info.Height = int(h)
				}
			}
		}
	} else if taken != "" {
		info.Taken = exifTime(taken, "")
	}
	if e, ok := ifd0[tagGPSIFD]; ok {
		if off, ok := t.uint(e); ok {
			info.Location = t.gps(t.ifd(off))
		}
	}
}

// gps reads latitude and longitude, degrees from three rationals and a
// hemisphere; anything incomplete or out of range is no location.
func (t *tiff) gps(g map[uint16]entry) *Location {
	coord := func(ref, val uint16, pos, neg string, limit float64) (float64, bool) {
		var deg float64
		for i, scale := range []float64{1, 60, 3600} {
			n, d, ok := t.rational(g[val], i)
			if !ok {
				return 0, false
			}
			deg += n / d / scale
		}
		switch strings.ToUpper(t.str(g[ref])) {
		case pos:
		case neg:
			deg = -deg
		default:
			return 0, false
		}
		return deg, math.Abs(deg) <= limit
	}
	lat, ok1 := coord(1, 2, "N", "S", 90)
	lon, ok2 := coord(3, 4, "E", "W", 180)
	if !ok1 || !ok2 || (lat == 0 && lon == 0) {
		return nil
	}
	return &Location{Latitude: math.Round(lat*1e6) / 1e6, Longitude: math.Round(lon*1e6) / 1e6}
}

// exifTime turns "2024:05:01 10:00:00" into "2024-05-01T10:00:00", with the
// camera's zone offset when it wrote one; otherwise as it is, cleaned.
func exifTime(s, offset string) string {
	if len(s) >= 19 && s[4] == ':' && s[7] == ':' && s[10] == ' ' {
		out := s[:4] + "-" + s[5:7] + "-" + s[8:10] + "T" + s[11:19]
		if len(offset) == 6 && (offset[0] == '+' || offset[0] == '-') && offset[3] == ':' {
			out += offset
		}
		if strings.HasPrefix(out, "0000") {
			return ""
		}
		return out
	}
	return s
}

// exposure writes an exposure time as photographers do: "1/250", "0.5",
// "2".
func exposure(n, d float64) string {
	v := n / d
	switch {
	case v >= 1:
		return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
	case v >= 0.3:
		return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
	default:
		return fmt.Sprintf("1/%d", int(math.Round(1/v)))
	}
}
