// Package zipread reads ZIP files as they lie on a share: their table of
// contents, and one entry's bytes. Office documents, OpenDocument files and
// EPUB books are ZIP files; so are the archives a person browses.
//
// It is ferry's own and not archive/zip, because the bytes come from
// whoever put the file there and this runs inside ferry: the central
// directory is read whole only when its size, its entry count and every
// offset in it fit the file and the bounds below; ZIP64's 64-bit fields are
// checked the same way; an entry is read only by Store or Deflate, never
// when encrypted; and its data never yields more than the size its header
// declares — a deflate stream that would is cut off with an error, so a
// small file cannot unpack into a large one unnoticed.
package zipread

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

var (
	// ErrNotZip is a file that is no ZIP this package reads.
	ErrNotZip = errors.New("zipread: not a ZIP file")
	// ErrUnsupported is an entry it cannot read: encrypted, or compressed
	// otherwise than Store or Deflate.
	ErrUnsupported = errors.New("zipread: the entry cannot be read")
	// ErrTooLarge is a directory or an entry beyond the bounds asked for.
	ErrTooLarge = errors.New("zipread: too large")
	// ErrCorrupt is an entry whose data does not match its header.
	ErrCorrupt = errors.New("zipread: the entry is damaged")
)

// Bounds on the table of contents.
const (
	// MaxDirectory is the most bytes of central directory read.
	MaxDirectory = 16 << 20
	// tail is how far from the end the end-of-directory record may be: its
	// own 22 bytes and a comment of at most 65535.
	tail = 22 + 65535
)

// Entry is one name in the table of contents.
type Entry struct {
	Name     string // as the archive writes it: slash-separated, and only text
	Dir      bool
	Method   uint16
	Flags    uint16
	Packed   int64 // compressed size
	Size     int64 // uncompressed size, as declared
	Modified time.Time
	// Link: the archive was made on Unix and says the entry is a symbolic
	// link — its data is the path it points to, not a file's content.
	Link   bool
	header int64 // where the entry's local header is
}

// Encrypted says whether the entry's data is encrypted.
func (e Entry) Encrypted() bool { return e.Flags&1 != 0 }

// Readable says whether Open can read the entry.
func (e Entry) Readable() bool {
	return !e.Encrypted() && (e.Method == 0 || e.Method == 8)
}

// List reads a ZIP file's table of contents: at most max entries (beyond is
// ErrTooLarge), in the archive's order.
func List(r io.ReaderAt, size int64, max int) ([]Entry, error) {
	if size < 22 {
		return nil, ErrNotZip
	}
	n := min(size, tail)
	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, size-n); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	end := bytes.LastIndex(buf, []byte("PK\x05\x06"))
	if end < 0 || len(buf)-end < 22 {
		return nil, ErrNotZip
	}
	rec := buf[end:]
	count := int64(binary.LittleEndian.Uint16(rec[10:12]))
	dirSize := int64(binary.LittleEndian.Uint32(rec[12:16]))
	dirAt := int64(binary.LittleEndian.Uint32(rec[16:20]))
	if count == 0xFFFF || dirSize == 0xFFFFFFFF || dirAt == 0xFFFFFFFF {
		// ZIP64: the real numbers are in the ZIP64 end record, which its
		// locator, just before, points to.
		at := size - n + int64(end) - 20
		if at < 0 {
			return nil, ErrNotZip
		}
		loc := make([]byte, 20)
		if _, err := r.ReadAt(loc, at); err != nil || string(loc[:4]) != "PK\x06\x07" {
			return nil, ErrNotZip
		}
		at64 := int64(binary.LittleEndian.Uint64(loc[8:16]))
		if at64 < 0 || at64 > size-56 {
			return nil, ErrNotZip
		}
		r64 := make([]byte, 56)
		if _, err := r.ReadAt(r64, at64); err != nil || string(r64[:4]) != "PK\x06\x06" {
			return nil, ErrNotZip
		}
		count = int64(binary.LittleEndian.Uint64(r64[32:40]))
		dirSize = int64(binary.LittleEndian.Uint64(r64[40:48]))
		dirAt = int64(binary.LittleEndian.Uint64(r64[48:56]))
	}
	if count < 0 || dirSize < 0 || dirAt < 0 || dirAt > size || dirSize > size-dirAt {
		return nil, ErrNotZip
	}
	if count > int64(max) {
		return nil, fmt.Errorf("%w: %d entries", ErrTooLarge, count)
	}
	if dirSize > MaxDirectory {
		return nil, ErrTooLarge
	}
	if dirSize < 46*count {
		return nil, ErrNotZip // more entries claimed than the directory holds
	}
	dir := make([]byte, dirSize)
	if _, err := r.ReadAt(dir, dirAt); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	out := make([]Entry, 0, count)
	for i := int64(0); i < count; i++ {
		if len(dir) < 46 || string(dir[:4]) != "PK\x01\x02" {
			return nil, ErrNotZip
		}
		le := binary.LittleEndian
		e := Entry{
			Flags:  le.Uint16(dir[8:10]),
			Method: le.Uint16(dir[10:12]),
			Packed: int64(le.Uint32(dir[20:24])),
			Size:   int64(le.Uint32(dir[24:28])),
			header: int64(le.Uint32(dir[42:46])),
		}
		e.Modified = dosTime(le.Uint16(dir[14:16]), le.Uint16(dir[12:14]))
		// The external attributes hold a Unix mode in their high half when
		// the archive was made on Unix (version made by, high byte 3).
		if dir[5] == 3 {
			e.Link = (le.Uint32(dir[38:42])>>16)&0o170000 == 0o120000
		}
		nameLen, extraLen, commentLen := int(le.Uint16(dir[28:30])), int(le.Uint16(dir[30:32])), int(le.Uint16(dir[32:34]))
		if len(dir) < 46+nameLen+extraLen+commentLen {
			return nil, ErrNotZip
		}
		name := dir[46 : 46+nameLen]
		extra := dir[46+nameLen : 46+nameLen+extraLen]
		if !zip64(&e, extra) {
			return nil, ErrNotZip
		}
		e.Name = textName(name, e.Flags)
		e.Dir = len(e.Name) > 0 && e.Name[len(e.Name)-1] == '/'
		if e.header < 0 || e.header > size || e.Packed < 0 || e.Size < 0 || e.Packed > size {
			return nil, ErrNotZip
		}
		out = append(out, e)
		dir = dir[46+nameLen+extraLen+commentLen:]
	}
	return out, nil
}

// zip64 takes the 64-bit sizes and offset from the ZIP64 extra field, for
// those the record set to all ones; false when it is missing or short.
func zip64(e *Entry, extra []byte) bool {
	need := e.Size == 0xFFFFFFFF || e.Packed == 0xFFFFFFFF || e.header == 0xFFFFFFFF
	if !need {
		return true
	}
	for len(extra) >= 4 {
		id, n := binary.LittleEndian.Uint16(extra[0:2]), int(binary.LittleEndian.Uint16(extra[2:4]))
		if len(extra) < 4+n {
			return false
		}
		if id == 0x0001 {
			f := extra[4 : 4+n]
			take := func(v *int64) bool {
				if *v != 0xFFFFFFFF {
					return true
				}
				if len(f) < 8 {
					return false
				}
				u := binary.LittleEndian.Uint64(f[:8])
				if u > 1<<62 {
					return false
				}
				*v, f = int64(u), f[8:]
				return true
			}
			return take(&e.Size) && take(&e.Packed) && take(&e.header)
		}
		extra = extra[4+n:]
	}
	return false
}

// textName is an entry's name as text: UTF-8 when the archive says so or
// it is valid, otherwise read as the code page 437 old archives use. It is
// only ever shown, never used as a path.
func textName(b []byte, flags uint16) string {
	if flags&0x800 != 0 || utf8.Valid(b) {
		return string(bytes.ToValidUTF8(b, []byte("�")))
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = cp437[c]
	}
	return string(r)
}

// Open returns a reader of the entry's bytes, uncompressed, that yields
// exactly its declared size: more than that, or less, is ErrCorrupt, and a
// declared size above limit is ErrTooLarge (limit <= 0: no limit).
func Open(r io.ReaderAt, size int64, e Entry, limit int64) (io.Reader, error) {
	if !e.Readable() {
		return nil, ErrUnsupported
	}
	if limit > 0 && e.Size > limit {
		return nil, ErrTooLarge
	}
	var lh [30]byte
	if e.header > size-30 {
		return nil, ErrCorrupt
	}
	if _, err := r.ReadAt(lh[:], e.header); err != nil {
		return nil, err
	}
	if string(lh[:4]) != "PK\x03\x04" {
		return nil, ErrCorrupt
	}
	data := e.header + 30 + int64(binary.LittleEndian.Uint16(lh[26:28])) + int64(binary.LittleEndian.Uint16(lh[28:30]))
	if data > size || e.Packed > size-data {
		return nil, ErrCorrupt
	}
	packed := io.NewSectionReader(r, data, e.Packed)
	var body io.Reader = packed
	if e.Method == 8 {
		body = flate.NewReader(packed)
	} else if e.Packed != e.Size {
		return nil, ErrCorrupt
	}
	return &exact{r: body, left: e.Size}, nil
}

// exact yields what its reader gives, and fails when that is more or less
// than the size declared.
type exact struct {
	r    io.Reader
	left int64
	err  error
}

func (x *exact) Read(p []byte) (int, error) {
	if x.err != nil {
		return 0, x.err
	}
	if x.left == 0 {
		// One more byte would be more than declared.
		var one [1]byte
		if n, _ := x.r.Read(one[:]); n > 0 {
			x.err = ErrCorrupt
			return 0, x.err
		}
		return 0, io.EOF
	}
	if int64(len(p)) > x.left {
		p = p[:x.left]
	}
	n, err := x.r.Read(p)
	x.left -= int64(n)
	if errors.Is(err, io.EOF) && x.left > 0 {
		x.err = ErrCorrupt
		return n, x.err
	}
	if err != nil && !errors.Is(err, io.EOF) {
		x.err = ErrCorrupt
		return n, x.err
	}
	return n, nil
}

// ReadAll reads a whole entry, of at most limit bytes.
func ReadAll(r io.ReaderAt, size int64, e Entry, limit int64) ([]byte, error) {
	if limit <= 0 || e.Size > limit {
		return nil, ErrTooLarge
	}
	rd, err := Open(r, size, e, limit)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 0, e.Size)
	buf := bytes.NewBuffer(b)
	if _, err := io.Copy(buf, rd); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Find returns the entry of that exact name.
func Find(entries []Entry, name string) (Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

func dosTime(date, t uint16) time.Time {
	if date == 0 {
		return time.Time{}
	}
	return time.Date(int(date>>9)+1980, time.Month(date>>5&0xF), int(date&0x1F),
		int(t>>11), int(t>>5&0x3F), int(t&0x1F)*2, 0, time.Local)
}

// cp437 is the code page old ZIP archives write names in.
var cp437 = [256]rune{
	0x0000, 0x263A, 0x263B, 0x2665, 0x2666, 0x2663, 0x2660, 0x2022, 0x25D8, 0x25CB, 0x25D9, 0x2642, 0x2640, 0x266A, 0x266B, 0x263C,
	0x25BA, 0x25C4, 0x2195, 0x203C, 0x00B6, 0x00A7, 0x25AC, 0x21A8, 0x2191, 0x2193, 0x2192, 0x2190, 0x221F, 0x2194, 0x25B2, 0x25BC,
	' ', '!', '"', '#', '$', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
	'@', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
	'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', '[', '\\', ']', '^', '_',
	'`', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', '{', '|', '}', '~', 0x2302,
	0x00C7, 0x00FC, 0x00E9, 0x00E2, 0x00E4, 0x00E0, 0x00E5, 0x00E7, 0x00EA, 0x00EB, 0x00E8, 0x00EF, 0x00EE, 0x00EC, 0x00C4, 0x00C5,
	0x00C9, 0x00E6, 0x00C6, 0x00F4, 0x00F6, 0x00F2, 0x00FB, 0x00F9, 0x00FF, 0x00D6, 0x00DC, 0x00A2, 0x00A3, 0x00A5, 0x20A7, 0x0192,
	0x00E1, 0x00ED, 0x00F3, 0x00FA, 0x00F1, 0x00D1, 0x00AA, 0x00BA, 0x00BF, 0x2310, 0x00AC, 0x00BD, 0x00BC, 0x00A1, 0x00AB, 0x00BB,
	0x2591, 0x2592, 0x2593, 0x2502, 0x2524, 0x2561, 0x2562, 0x2556, 0x2555, 0x2563, 0x2551, 0x2557, 0x255D, 0x255C, 0x255B, 0x2510,
	0x2514, 0x2534, 0x252C, 0x251C, 0x2500, 0x253C, 0x255E, 0x255F, 0x255A, 0x2554, 0x2569, 0x2566, 0x2560, 0x2550, 0x256C, 0x2567,
	0x2568, 0x2564, 0x2565, 0x2559, 0x2558, 0x2552, 0x2553, 0x256B, 0x256A, 0x2518, 0x250C, 0x2588, 0x2584, 0x258C, 0x2590, 0x2580,
	0x03B1, 0x00DF, 0x0393, 0x03C0, 0x03A3, 0x03C3, 0x00B5, 0x03C4, 0x03A6, 0x0398, 0x03A9, 0x03B4, 0x221E, 0x03C6, 0x03B5, 0x2229,
	0x2261, 0x00B1, 0x2265, 0x2264, 0x2320, 0x2321, 0x00F7, 0x2248, 0x00B0, 0x2219, 0x00B7, 0x221A, 0x207F, 0x00B2, 0x25A0, 0x00A0,
}
