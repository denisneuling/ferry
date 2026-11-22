package meta

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"runtime"
	"testing"
)

// The files here are built byte by byte: a tag around a small PNG, the way
// the taggers write them, and no recording at all behind it.

func testPNG(t testing.TB, w int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, 7))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ss(n int) []byte { // syncsafe
	return []byte{byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
}

func be32(n int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(n)) }
func le32(n int) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(n)) }

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// apic is an APIC frame's body: Latin-1, a MIME type, a picture type, a
// description.
func apic(typ byte, desc string, pic []byte) []byte {
	return cat([]byte{0}, []byte("image/png\x00"), []byte{typ}, []byte(desc+"\x00"), pic)
}

func id3Frame(major byte, id string, body []byte, flags byte) []byte {
	size := be32(len(body))
	if major == 4 {
		size = ss(len(body))
	}
	return cat([]byte(id), size, []byte{0, flags}, body)
}

func id3Tag(major, flags byte, frames ...[]byte) []byte {
	body := cat(frames...)
	body = append(body, make([]byte, 64)...) // padding
	return cat([]byte("ID3"), []byte{major, 0, flags}, ss(len(body)), body, []byte{0xFF, 0xFB, 0x90, 0x00})
}

func unsync(b []byte) []byte {
	var out []byte
	for i, c := range b {
		out = append(out, c)
		if c == 0xFF && (i+1 == len(b) || b[i+1]&0xE0 == 0xE0 || b[i+1] == 0) {
			out = append(out, 0)
		}
	}
	return out
}

func flacFile(blocks ...[]byte) []byte {
	out := []byte("fLaC")
	all := append([][]byte{cat([]byte{0}, make([]byte, 34))}, blocks...)
	for i, b := range all {
		h := b[0]
		if i == len(all)-1 {
			h |= 0x80
		}
		n := len(b) - 1
		out = append(out, h, byte(n>>16), byte(n>>8), byte(n))
		out = append(out, b[1:]...)
	}
	return out
}

// pictureBlock is the structure FLAC's PICTURE and Ogg's
// METADATA_BLOCK_PICTURE share.
func pictureBlock(typ int, pic []byte) []byte {
	mime, desc := "image/png", "Cover"
	return cat(be32(typ), be32(len(mime)), []byte(mime), be32(len(desc)), []byte(desc),
		be32(7), be32(7), be32(8), be32(0), be32(len(pic)), pic)
}

func atom(typ string, body ...[]byte) []byte {
	b := cat(body...)
	return cat(be32(8+len(b)), []byte(typ), b)
}

func mp4File(meta []byte) []byte {
	return cat(atom("ftyp", []byte("M4A \x00\x00\x00\x00")), atom("mdat", make([]byte, 100)),
		atom("moov", atom("mvhd", make([]byte, 100)), atom("udta", meta)))
}

// oggFile puts packets into pages of at most segs segments each.
func oggFile(segs int, packets ...[]byte) []byte {
	var lacing []byte
	var data []byte
	var out []byte
	seq := 0
	flush := func() {
		if len(lacing) == 0 {
			return
		}
		h := cat([]byte("OggS\x00\x00"), make([]byte, 8), le32(0x1234), le32(seq), make([]byte, 4), []byte{byte(len(lacing))})
		out = append(out, cat(h, lacing, data)...)
		lacing, data = nil, nil
		seq++
	}
	for _, p := range packets {
		for rest := len(p); ; rest -= 255 {
			if len(lacing) == segs {
				flush()
			}
			if rest >= 255 {
				lacing = append(lacing, 255)
				continue
			}
			lacing = append(lacing, byte(rest))
			break
		}
		data = append(data, p...)
	}
	flush()
	return out
}

func vorbisComment(prefix string, comments ...string) []byte {
	out := cat([]byte(prefix), le32(6), []byte("tester"), le32(len(comments)))
	for _, c := range comments {
		out = append(out, cat(le32(len(c)), []byte(c))...)
	}
	return out
}

func readCover(t *testing.T, file []byte, most int64) ([]byte, error) {
	t.Helper()
	c, err := FindCover(bytes.NewReader(file), int64(len(file)), most, nil)
	if err != nil {
		return nil, err
	}
	got, err := io.ReadAll(c.Reader())
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(got)) != c.Size {
		t.Fatalf("the cover says %d bytes and has %d", c.Size, len(got))
	}
	return got, nil
}

type coverCase struct {
	name string
	file []byte
	want []byte
}

// coverCases are a file of every format and variant FindCover reads, and
// the picture it must find in each; the fuzz test starts from them.
func coverCases(t testing.TB) []coverCase {
	front, back := testPNG(t, 11), testPNG(t, 13)
	// A picture with 0xFF bytes followed by others that unsynchronisation
	// changes, so that undoing it matters.
	ffs := append(append([]byte{}, front...), 0xFF, 0xE0, 0xFF, 0x00, 0xFF)
	b64 := base64.StdEncoding.EncodeToString
	block := pictureBlock(3, front)
	big := testPNG(t, 4000) // large enough to span several Ogg pages

	return []coverCase{
		{"id3v2.3", id3Tag(3, 0, id3Frame(3, "TIT2", []byte("\x00A title"), 0), id3Frame(3, "APIC", apic(3, "Front", front), 0)), front},
		{"id3v2.4", id3Tag(4, 0, id3Frame(4, "APIC", apic(3, "", front), 0)), front},
		{"id3v2.2", id3Tag(2, 0, cat([]byte("PIC"), []byte{0, 0, byte(6 + len(front))}, []byte("\x00PNG\x03\x00"), front)), front},
		{"the front cover first", id3Tag(3, 0, id3Frame(3, "APIC", apic(4, "Back", back), 0), id3Frame(3, "APIC", apic(3, "Front", front), 0)), front},
		{"any picture without a front cover", id3Tag(3, 0, id3Frame(3, "APIC", apic(4, "Back", back), 0)), back},
		{"a UTF-16 description", id3Tag(3, 0, id3Frame(3, "APIC", cat([]byte{1}, []byte("image/png\x00\x03"), []byte{0xFF, 0xFE, 'F', 0, 0, 0}, front), 0)), front},
		{"id3v2.4 grouping and a data length", id3Tag(4, 0, id3Frame(4, "APIC", cat([]byte{9}, ss(0), apic(3, "", front)), 0x41)), front},
		{"id3v2.4 frame unsynchronisation", id3Tag(4, 0, id3Frame(4, "APIC", unsync(apic(3, "", ffs)), 0x02)), ffs},
		{"id3v2.3 tag unsynchronisation", func() []byte {
			body := unsync(cat(id3Frame(3, "APIC", apic(3, "", ffs), 0), make([]byte, 16)))
			return cat([]byte("ID3\x03\x00\x80"), ss(len(body)), body)
		}(), ffs},
		{"flac", flacFile(cat([]byte{4}, vorbisComment("", "TITLE=x")), cat([]byte{6}, block)), front},
		{"flac front cover second", flacFile(cat([]byte{6}, pictureBlock(0, back)), cat([]byte{6}, block)), front},
		{"m4a", mp4File(atom("meta", make([]byte, 4), atom("hdlr", make([]byte, 25)),
			atom("ilst", atom("\xa9nam", atom("data", make([]byte, 12))), atom("covr", atom("data", be32(14), be32(0), front))))), front},
		{"m4a, QuickTime meta", mp4File(atom("meta", atom("hdlr", make([]byte, 25)),
			atom("ilst", atom("covr", atom("data", be32(13), be32(0), front))))), front},
		{"ogg vorbis", oggFile(255, cat([]byte("\x01vorbis"), make([]byte, 23)),
			append(vorbisComment("\x03vorbis", "TITLE=x", "METADATA_BLOCK_PICTURE="+b64(block)), 1)), front},
		{"opus over several pages", oggFile(8, cat([]byte("OpusHead"), make([]byte, 11)),
			vorbisComment("OpusTags", "metadata_block_picture="+b64(pictureBlock(3, big)))), big},
	}
}

func TestFindCover(t *testing.T) {
	for _, c := range coverCases(t) {
		t.Run(c.name, func(t *testing.T) {
			got, err := readCover(t, c.file, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("got %d bytes, not the picture", len(got))
			}
			if _, err := readCover(t, c.file, int64(len(c.want)-1)); !errors.Is(err, ErrCoverTooLarge) {
				t.Fatalf("a limit below the picture: %v", err)
			}
			// Cut anywhere, a file yields the cover or none, and never panics.
			for n := 0; n < len(c.file); n += 1 + len(c.file)/97 {
				if _, err := readCover(t, c.file[:n], 1<<20); err != nil && !errors.Is(err, ErrNoCover) {
					t.Fatalf("cut at %d: %v", n, err)
				}
			}
		})
	}
}

func TestFindCoverNone(t *testing.T) {
	for name, file := range map[string][]byte{
		"an mp3 without a picture":  id3Tag(3, 0, id3Frame(3, "TIT2", []byte("\x00A title"), 0)),
		"a compressed picture":      id3Tag(3, 0, id3Frame(3, "APIC", apic(3, "", testPNG(t, 3)), 0x80)),
		"a flac without a picture":  flacFile(cat([]byte{4}, vorbisComment("", "TITLE=x"))),
		"an mp4 without tags":       mp4File(atom("free")),
		"an ogg without a picture":  oggFile(255, cat([]byte("\x01vorbis"), make([]byte, 23)), vorbisComment("\x03vorbis", "TITLE=x")),
		"a picture block in a lie":  flacFile(cat([]byte{6}, be32(3), be32(1<<30))),
		"not a recording":           []byte("just some text, long enough to read a header from"),
		"nothing":                   nil,
		"an mp4 box claiming a lot": cat(atom("ftyp", []byte("M4A ")), be32(0xFFFFFFF0), []byte("moov")),
	} {
		if _, err := readCover(t, file, 1<<20); !errors.Is(err, ErrNoCover) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestIsAudio(t *testing.T) {
	for h, want := range map[string]bool{
		"ID3\x03": true, "fLaC\x00": true, "OggS\x00": true, "\x00\x00\x00\x20ftypM4A ": true,
		"\xFF\xD8\xFF": false, "%PDF-1.7": false, "": false,
	} {
		if IsAudio([]byte(h)) != want {
			t.Errorf("IsAudio(%q) is not %v", h, want)
		}
	}
}

// The gate is passed before a picture is copied into memory, and only then;
// its refusal ends the search.
func TestFindCoverGate(t *testing.T) {
	refused := errors.New("refused")
	for _, c := range coverCases(t) {
		calls := 0
		cover, err := FindCover(bytes.NewReader(c.file), int64(len(c.file)), 1<<20, func() error { calls++; return nil })
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		_, plain := cover.r.(*io.SectionReader)
		if plain == (calls != 0) || calls > 1 {
			t.Errorf("%s: the gate was passed %d times for a cover read in place: %v", c.name, calls, plain)
		}
		if !plain {
			if _, err := FindCover(bytes.NewReader(c.file), int64(len(c.file)), 1<<20, func() error { return refused }); !errors.Is(err, refused) {
				t.Errorf("%s: a refusing gate: %v", c.name, err)
			}
		}
	}
}

// countingReader counts what is read from it.
type countingReader struct {
	r    io.ReaderAt
	read int64
}

func (c *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.read += int64(n)
	return n, err
}

// FuzzFindCover feeds FindCover what its length fields — ID3 sizes, FLAC
// block lengths, MP4 boxes, Ogg lacing, base64 — make of arbitrary bytes.
// A panic fails it (FindCover's own recover is off), and so does reading
// beyond the budget, allocating beyond the bound below, a cover larger than
// the limit, one held in memory beyond inMemory, or one in place that lies
// outside the file.
func FuzzFindCover(f *testing.F) {
	for _, c := range coverCases(f) {
		f.Add(c.file)
	}
	catchPanics = false
	f.Cleanup(func() { catchPanics = true })
	const most = 1 << 20
	// What one search may allocate at worst: every byte it may read (each
	// read is a new buffer), the Ogg packet as it grows by doubling, and
	// the picture decoded from it or from unsynchronisation.
	const allocBound = Budget + 2*inMemory + 3*(inMemory*4/3+64<<10) + 2*inMemory
	f.Fuzz(func(t *testing.T, b []byte) {
		r := &countingReader{r: bytes.NewReader(b)}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		c, err := FindCover(r, int64(len(b)), most, nil)
		runtime.ReadMemStats(&after)
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc > allocBound {
			t.Fatalf("allocated %d bytes", alloc)
		}
		if r.read > Budget+2*inMemory {
			t.Fatalf("read %d bytes", r.read)
		}
		if err != nil {
			if !errors.Is(err, ErrNoCover) && !errors.Is(err, ErrCoverTooLarge) {
				t.Fatalf("an error of its own: %v", err)
			}
			return
		}
		if c.Size <= 0 || c.Size > most {
			t.Fatalf("a cover of %d bytes", c.Size)
		}
		if sr, ok := c.r.(*io.SectionReader); ok {
			if _, off, n := sr.Outer(); off < 0 || n != c.Size || off+n > int64(len(b)) {
				t.Fatalf("a cover at %d+%d in %d bytes", off, n, len(b))
			}
		} else if c.Size > inMemory {
			t.Fatalf("a cover of %d bytes in memory", c.Size)
		}
		got, err := io.ReadAll(c.Reader())
		if err != nil || int64(len(got)) != c.Size {
			t.Fatalf("read %d of %d bytes: %v", len(got), c.Size, err)
		}
	})
}
