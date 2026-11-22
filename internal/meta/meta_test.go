package meta

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// sparse is a file of size bytes: data at its offsets, zeros elsewhere. It
// counts what is read, so a test can hold a format to its budget without
// holding the file in memory.
type sparse struct {
	size  int64
	parts map[int64][]byte
	read  int64
}

func (s *sparse) ReadAt(p []byte, off int64) (int, error) {
	n := len(p)
	if off+int64(n) > s.size {
		n = int(s.size - off)
	}
	clear(p[:n])
	for at, d := range s.parts {
		for i := range d {
			if o := at + int64(i) - off; o >= 0 && o < int64(n) {
				p[o] = d[i]
			}
		}
	}
	s.read += int64(n)
	return n, nil
}

func whole(b []byte) *sparse { return &sparse{size: int64(len(b)), parts: map[int64][]byte{0: b}} }

// tiffBuilder writes a little-endian TIFF with IFD0, an EXIF IFD and a GPS
// IFD, enough for the tags ferry reads.
type tag struct {
	id, typ uint16
	val     []byte
	count   uint32
}

func ascii(id uint16, s string) tag { return tag{id, 2, append([]byte(s), 0), uint32(len(s) + 1)} }
func short(id uint16, v uint16) tag {
	return tag{id, 3, binary.LittleEndian.AppendUint16(nil, v), 1}
}
func long(id uint16, v uint32) tag { return tag{id, 4, binary.LittleEndian.AppendUint32(nil, v), 1} }
func rationals(id uint16, vs ...[2]uint32) tag {
	var b []byte
	for _, v := range vs {
		b = binary.LittleEndian.AppendUint32(b, v[0])
		b = binary.LittleEndian.AppendUint32(b, v[1])
	}
	return tag{id, 5, b, uint32(len(vs))}
}

func buildTIFF(ifd0, exif, gps []tag) []byte {
	b := []byte("II*\x00\x08\x00\x00\x00")
	// Lay out the three IFDs one after another, values after each.
	write := func(tags []tag, at int) []byte {
		out := binary.LittleEndian.AppendUint16(nil, uint16(len(tags)))
		valAt := at + 2 + 12*len(tags) + 4
		var vals []byte
		for _, t := range tags {
			out = binary.LittleEndian.AppendUint16(out, t.id)
			out = binary.LittleEndian.AppendUint16(out, t.typ)
			out = binary.LittleEndian.AppendUint32(out, t.count)
			if len(t.val) <= 4 {
				v := make([]byte, 4)
				copy(v, t.val)
				out = append(out, v...)
			} else {
				out = binary.LittleEndian.AppendUint32(out, uint32(valAt+len(vals)))
				vals = append(vals, t.val...)
			}
		}
		out = append(out, 0, 0, 0, 0)
		return append(out, vals...)
	}
	size := func(tags []tag) int { return len(write(tags, 0)) }
	exifAt := 8 + size(append(ifd0, long(tagExifIFD, 0), long(tagGPSIFD, 0)))
	gpsAt := exifAt + size(exif)
	ifd0 = append(ifd0, long(tagExifIFD, uint32(exifAt)), long(tagGPSIFD, uint32(gpsAt)))
	b = append(b, write(ifd0, 8)...)
	b = append(b, write(exif, exifAt)...)
	return append(b, write(gps, gpsAt)...)
}

func cameraTIFF() []byte {
	return buildTIFF(
		[]tag{ascii(tagMake, "Fictional Optics"), ascii(tagModel, "FO-1 Mark II"), short(tagOrientation, 6)},
		[]tag{
			ascii(tagTaken, "2025:08:14 18:42:07"), ascii(tagTakenOffset, "+02:00"),
			rationals(tagFNumber, [2]uint32{28, 10}), rationals(tagExposure, [2]uint32{1, 250}),
			short(tagISO, 400), rationals(tagFocal, [2]uint32{350, 10}), ascii(tagLensModel, "35mm F2"),
			short(tagPixelX, 9999), short(tagPixelY, 9999),
		},
		[]tag{
			ascii(1, "N"), rationals(2, [2]uint32{54, 1}, [2]uint32{19, 1}, [2]uint32{30, 1}),
			ascii(3, "E"), rationals(4, [2]uint32{10, 1}, [2]uint32{8, 1}, [2]uint32{0, 1}),
		},
	)
}

func jpegWithEXIF(t *testing.T, w, h int, exif []byte) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	j := buf.Bytes()
	seg := append([]byte("Exif\x00\x00"), exif...)
	app1 := append([]byte{0xFF, 0xE1}, byte((len(seg)+2)>>8), byte(len(seg)+2))
	app1 = append(app1, seg...)
	return append(append(append([]byte{}, j[:2]...), app1...), j[2:]...)
}

func TestJPEG(t *testing.T) {
	f := jpegWithEXIF(t, 64, 48, cameraTIFF())
	got := Read(whole(f), int64(len(f)), "IMG_0001.jpg")
	want := Info{
		Kind: KindImage, Format: "JPEG", Width: 48, Height: 64, // orientation 6: turned
		Taken: "2025-08-14T18:42:07+02:00", Camera: "Fictional Optics FO-1 Mark II", Lens: "35mm F2",
		FNumber: 2.8, Exposure: "1/250", ISO: 400, FocalLength: 35, Orientation: 6,
		Location: &Location{Latitude: 54.325, Longitude: 10.133333},
	}
	if got.Location == nil || *got.Location != *want.Location {
		t.Fatalf("location %+v", got.Location)
	}
	got.Location, want.Location = nil, nil
	if got != want {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestImagesWithoutEXIF(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 30, 20), color.Palette{color.Black, color.White})
	var p, g bytes.Buffer
	_ = png.Encode(&p, img)
	_ = gif.Encode(&g, img, nil)
	bmp := make([]byte, 54)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[18:], 30)
	binary.LittleEndian.PutUint32(bmp[22:], uint32(0xFFFFFFFF-19)) // -20: top-down
	webp := []byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x00\x00\x00\x00")
	webp = append(webp, 29, 0, 0, 19, 0, 0) // 30 × 20, stored minus one
	for name, f := range map[string][]byte{"PNG": p.Bytes(), "GIF": g.Bytes(), "BMP": bmp, "WebP": webp} {
		got := Read(whole(f), int64(len(f)), "x")
		if got.Kind != KindImage || got.Format != name || got.Width != 30 || got.Height != 20 {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// mp4box writes a box.
func mp4box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(binary.BigEndian.AppendUint32([]byte{}, uint32(8+len(body))), append([]byte(typ), body...)...)
}

func full(version byte, rest ...byte) []byte { return append([]byte{version, 0, 0, 0}, rest...) }

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// TestMP4IndexAtTheEnd: an MP4 whose moov follows 50 MB of media data, as a
// camera writes it. The media data is passed over by its header, and the
// read stays far within the budget.
func TestMP4IndexAtTheEnd(t *testing.T) {
	mvhd := mp4box("mvhd", full(0, make([]byte, 8)...), u32(1000), u32(93500), make([]byte, 80))
	tkhd := mp4box("tkhd", full(0, make([]byte, 72)...), u32(1920<<16), u32(1080<<16))
	stsdVideo := mp4box("stsd", full(0), u32(1), u32(86), []byte("avc1"), make([]byte, 78))
	audioEntry := append(append(u32(36), []byte("mp4a")...), make([]byte, 16)...)
	audioEntry = append(audioEntry, 0, 2, 0, 16, 0, 0, 0, 0)
	audioEntry = append(audioEntry, u32(48000<<16)...)
	stsdAudio := mp4box("stsd", full(0), u32(1), audioEntry)
	trak := func(handler string, stsd []byte, extra ...[]byte) []byte {
		hdlr := mp4box("hdlr", full(0, 0, 0, 0, 0), []byte(handler), make([]byte, 12))
		return mp4box("trak", append(extra, mp4box("mdia", hdlr, mp4box("minf", mp4box("stbl", stsd))))...)
	}
	title := mp4box("\xa9nam", mp4box("data", u32(1), u32(0), []byte("Strandtag")))
	udta := mp4box("udta", mp4box("meta", full(0), mp4box("ilst", title)))
	moov := mp4box("moov", mvhd, trak("vide", stsdVideo, tkhd), trak("soun", stsdAudio), udta)
	ftyp := mp4box("ftyp", []byte("isom"), u32(0), []byte("isomiso2avc1mp41"))
	const media = 50 << 20
	mdatHdr := binary.BigEndian.AppendUint32(nil, uint32(8+media))
	mdatHdr = append(mdatHdr, "mdat"...)
	f := &sparse{size: int64(len(ftyp) + 8 + media + len(moov)), parts: map[int64][]byte{
		0:                            append(ftyp, mdatHdr...),
		int64(len(ftyp) + 8 + media): moov,
	}}
	got := Read(f, f.size, "clip.mp4")
	if got.Kind != KindVideo || got.Format != "MP4" || got.Width != 1920 || got.Height != 1080 ||
		got.Duration != 93500*time.Millisecond || got.Codec != "H.264 · AAC" ||
		got.Channels != 2 || got.SampleRate != 48000 || got.Title != "Strandtag" || got.Bitrate == 0 {
		t.Fatalf("%+v", got)
	}
	if f.read > 2*head {
		t.Fatalf("read %d bytes of a %d-byte file", f.read, f.size)
	}
}

func TestHEIC(t *testing.T) {
	exif := append(u32(0), cameraTIFF()...)
	ftyp := mp4box("ftyp", []byte("heic"), u32(0), []byte("mif1heic"))
	infe := mp4box("infe", full(2, 0, 2, 0, 0), []byte("Exif"), []byte{0})
	iinf := mp4box("iinf", full(0, 0, 1), infe)
	ispe := mp4box("ispe", full(0), u32(4032), u32(3024))
	small := mp4box("ispe", full(0), u32(512), u32(384))
	iprp := mp4box("iprp", mp4box("ipco", small, ispe))
	ilocFor := func(off uint32) []byte {
		// version 0, offset and length 4 bytes, no base offset, one item.
		return mp4box("iloc", full(0, 0x44, 0x00), []byte{0, 1}, []byte{0, 2}, []byte{0, 0}, []byte{0, 1}, u32(off), u32(uint32(len(exif))))
	}
	build := func(off uint32) []byte {
		meta := mp4box("meta", full(0), mp4box("hdlr", full(0, 0, 0, 0, 0), []byte("pict"), make([]byte, 12)), iinf, ilocFor(off), iprp)
		return append(ftyp, meta...)
	}
	head := build(0)
	off := uint32(len(head) + 8)
	f := append(build(off), mp4box("mdat", exif)...)
	got := Read(whole(f), int64(len(f)), "IMG_1234.HEIC")
	if got.Format != "HEIC" || got.Width != 3024 || got.Height != 4032 || // stored 4032×3024, orientation 6 got.Camera != "Fictional Optics FO-1 Mark II" ||
		got.Taken != "2025-08-14T18:42:07+02:00" || got.Location == nil {
		t.Fatalf("%+v", got)
	}
}

func TestMP3(t *testing.T) {
	frame := func(id, text string) []byte {
		data := append([]byte{3}, text...) // UTF-8
		return append(append([]byte(id), u32(uint32(len(data)))...), append([]byte{0, 0}, data...)...)
	}
	tags := append(frame("TIT2", "Möwenlied"), frame("TPE1", "Die Fiktiven")...)
	n := len(tags)
	id3 := append([]byte("ID3\x03\x00\x00"), byte(n>>21&0x7F), byte(n>>14&0x7F), byte(n>>7&0x7F), byte(n&0x7F))
	// MPEG-1 Layer III, 128 kbit/s, 44.1 kHz, stereo, with a Xing header of
	// 7656 frames: 200 s.
	hdr := []byte{0xFF, 0xFB, 0x90, 0x00}
	xing := append(append(make([]byte, 32), "Xing"...), append(u32(1), u32(7656)...)...)
	f := append(append(append(id3, tags...), hdr...), xing...)
	f = append(f, make([]byte, 3_200_000)...)
	got := Read(whole(f), int64(len(f)), "lied.mp3")
	if got.Format != "MP3" || got.Title != "Möwenlied" || got.Author != "Die Fiktiven" || got.SampleRate != 44100 ||
		got.Channels != 2 || got.Duration.Round(time.Second) != 200*time.Second {
		t.Fatalf("%+v", got)
	}
}

func TestFLACAndWAV(t *testing.T) {
	si := make([]byte, 18)
	// 44100 Hz, 2 channels, 16 bit, 441000 samples: 10 s.
	si[10], si[11], si[12] = 0x0A, 0xC4, 0x42
	si[13] = 0xF0
	binary.BigEndian.PutUint32(si[14:], 441000)
	vc := binary.LittleEndian.AppendUint32(nil, 0)
	vc = binary.LittleEndian.AppendUint32(vc, 1)
	c := "TITLE=Brandung"
	vc = append(binary.LittleEndian.AppendUint32(vc, uint32(len(c))), c...)
	flac := append([]byte("fLaC\x00\x00\x00\x12"), si...)
	flac = append(append(flac, 0x84, 0, 0, byte(len(vc))), vc...)
	got := Read(whole(flac), int64(len(flac)), "x.flac")
	if got.Format != "FLAC" || got.SampleRate != 44100 || got.Channels != 2 || got.Duration != 10*time.Second || got.Title != "Brandung" {
		t.Fatalf("flac %+v", got)
	}

	fmtc := binary.LittleEndian.AppendUint16(nil, 1)
	fmtc = binary.LittleEndian.AppendUint16(fmtc, 1)
	fmtc = binary.LittleEndian.AppendUint32(fmtc, 8000)
	fmtc = binary.LittleEndian.AppendUint32(fmtc, 16000)
	fmtc = append(fmtc, 2, 0, 16, 0)
	chunk := func(id string, d []byte) []byte {
		return append(append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(d)))...), d...)
	}
	info := chunk("LIST", append([]byte("INFO"), chunk("INAM", []byte("Sprachnotiz\x00"))...))
	wav := append([]byte("RIFF\x00\x00\x00\x00WAVE"), chunk("fmt ", fmtc)...)
	wav = append(wav, info...)
	wav = append(wav, chunk("data", make([]byte, 48000))...)
	got = Read(whole(wav), int64(len(wav)), "x.wav")
	if got.Format != "WAV" || got.Codec != "PCM" || got.SampleRate != 8000 || got.Duration != 3*time.Second || got.Title != "Sprachnotiz" {
		t.Fatalf("wav %+v", got)
	}
}

func TestPDF(t *testing.T) {
	plain := "%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n" +
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 12 >> endobj\n" +
		"5 0 obj << /Type /Pages /Count 4 >> endobj\n" +
		"9 0 obj << /Title (Steuern \\(2025\\)) /Author <FEFF00C4006E006E0065> /Producer (FictionalWriter 3) >> endobj\n" +
		"trailer << /Root 1 0 R /Info 9 0 R >>\n%%EOF\n"
	got := Read(whole([]byte(plain)), int64(len(plain)), "x.pdf")
	if got.Format != "PDF 1.4" || got.Pages != 12 || got.Title != "Steuern (2025)" || got.Author != "Änne" || got.Producer != "FictionalWriter 3" {
		t.Fatalf("plain %+v", got)
	}

	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write([]byte("<< /Type /Pages /Kids [] /Count 37 >> << /Title (Compressed) >>"))
	_ = w.Close()
	objstm := "%PDF-1.7\n10 0 obj << /Type /ObjStm /N 2 /First 8 /Filter /FlateDecode /Length " +
		strings.Repeat("9", 1) + " >>\nstream\n" + z.String() + "\nendstream endobj\n%%EOF\n"
	got = Read(whole([]byte(objstm)), int64(len(objstm)), "x.pdf")
	if got.Pages != 37 || got.Title != "Compressed" {
		t.Fatalf("object stream %+v", got)
	}
}

func TestText(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       Info
	}{
		{"a.txt", "eins\r\nzwei\r\ndrei", Info{Kind: KindText, Format: "Text", Encoding: "ascii", Lines: 3, LineEndings: "CRLF"}},
		{"b.md", "Grüße\naus\n", Info{Kind: KindText, Format: "Text", Encoding: "utf-8", Lines: 2, LineEndings: "LF"}},
		{"c.csv", "\xEF\xBB\xBFa;b\n", Info{Kind: KindText, Format: "Text", Encoding: "utf-8-bom", Lines: 1, LineEndings: "LF"}},
		{"d.txt", "Gr\xfc\xdfe\n", Info{Kind: KindText, Format: "Text", Encoding: "8bit", Lines: 1, LineEndings: "LF"}},
	} {
		got := Read(whole([]byte(c.body)), int64(len(c.body)), c.name)
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
	if got := Read(whole([]byte("bin\x00ary")), 7, "x.dat"); got.Kind != KindOther {
		t.Errorf("binary as %+v", got)
	}
	long := &sparse{size: 10 << 20, parts: map[int64][]byte{0: []byte(strings.Repeat("line\n", 300_000))}}
	got := Read(long, long.size, "big.log")
	if !got.LinesAtLeast {
		t.Errorf("a long file's count is not a lower bound: %+v", got)
	}
}

// TestHostileFiles feeds every format's magic followed by random bytes: no
// panic escapes, nothing reads beyond the budget, and strings stay clean.
func TestHostileFiles(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	magics := [][]byte{
		{0xFF, 0xD8, 0xFF, 0xE1}, []byte("\x89PNG\r\n\x1a\n"), []byte("GIF89a"), []byte("RIFF\x00\x00\x00\x00WEBP"),
		[]byte("RIFF\x00\x00\x00\x00WAVE"), []byte("BM"), []byte("II*\x00"), []byte("MM\x00*"),
		[]byte("\x00\x00\x00\x18ftypheic"), []byte("\x00\x00\x00\x18ftypisom"), []byte("fLaC"), []byte("ID3\x04\x00"),
		{0xFF, 0xFB, 0x90, 0x00}, []byte("%PDF-1.7"),
	}
	for round := 0; round < 400; round++ {
		m := magics[round%len(magics)]
		f := append(append([]byte{}, m...), make([]byte, r.Intn(64<<10))...)
		r.Read(f[len(m):])
		s := &sparse{size: int64(len(f)) + int64(r.Intn(1<<30)), parts: map[int64][]byte{0: f}}
		got := Read(s, s.size, "x")
		if s.read > Budget {
			t.Fatalf("round %d read %d bytes", round, s.read)
		}
		for _, v := range []string{got.Title, got.Author, got.Camera, got.Lens, got.Producer, got.Codec, got.Taken} {
			if strings.ContainsAny(v, "\x00\x1b\r\n") || len([]rune(v)) > 201 {
				t.Fatalf("round %d: unclean %q", round, v)
			}
		}
	}
}

// FuzzRead holds Read to its promises on any bytes: no panic, the budget
// kept. go test -fuzz FuzzRead ./internal/meta runs it beyond the seeds.
func FuzzRead(f *testing.F) {
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 8, 'E', 'x', 'i', 'f', 0, 0})
	f.Add([]byte("\x00\x00\x00\x10ftypheic\x00\x00\x00\x00"))
	f.Add([]byte("%PDF-1.7 /Type /ObjStm stream\nx\nendstream"))
	f.Add([]byte("RIFF\x00\x00\x00\x00WAVEfmt \xff\xff\xff\xff"))
	f.Fuzz(func(t *testing.T, b []byte) {
		s := &sparse{size: int64(len(b)) * 1000, parts: map[int64][]byte{0: b}}
		_ = Read(s, s.size, "f")
		if s.read > Budget {
			t.Fatalf("read %d bytes", s.read)
		}
	})
}
