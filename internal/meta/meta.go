// Package meta reads what a file says about itself — an image's size and
// camera, a PDF's pages and title, a recording's length — from the file's
// own bytes, in pure Go and within a byte budget.
//
// It never decodes pixels or sound, never stores anything, and never fails
// on a malformed file: what it could read it returns, the rest it leaves
// out. Everything it returns came out of the file and is data, to be shown
// as text and never as markup.
package meta

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Budget is the most a Read takes from a file, in bytes. The head comes
// first; a format whose index lies at the end (an MP4 written without "fast
// start", a PDF's trailer) reads box headers and the tail within the same
// budget.
const Budget = 4 << 20

// head is how much of the start of a file is read to tell its format and to
// parse the formats that keep everything there.
const head = 1 << 20

// Kind is what sort of file it is.
type Kind string

const (
	KindImage Kind = "image"
	KindVideo Kind = "video"
	KindAudio Kind = "audio"
	KindPDF   Kind = "pdf"
	KindText  Kind = "text"
	KindOther Kind = "other"
)

// Location is a position from a photo's GPS tags.
type Location struct {
	Latitude  float64
	Longitude float64
}

// Info is what a file says about itself. Zero values are absent.
type Info struct {
	Kind   Kind
	Format string // e.g. "JPEG", "MP4", "FLAC"

	// Images and video, as shown: upright, after the EXIF orientation
	Width, Height int
	// Photos
	Taken       string // as the camera wrote it, ISO 8601 without a zone unless it gave one
	Camera      string
	Lens        string
	FNumber     float64 // f/2.8 is 2.8
	Exposure    string  // "1/250", "2"
	ISO         int
	FocalLength float64 // millimetres
	Orientation int     // EXIF 1–8
	Location    *Location

	// Documents and tags
	Pages    int
	Title    string
	Author   string // a PDF's author, a recording's artist
	Album    string
	Producer string // the program that made a PDF

	// Recordings
	Duration   time.Duration
	Codec      string
	Bitrate    int // bits per second
	SampleRate int
	Channels   int

	// Text
	Encoding     string // ascii, utf-8, utf-8-bom, utf-16le, utf-16be, 8bit
	Lines        int
	LinesAtLeast bool   // the file is longer than what was counted
	LineEndings  string // "LF", "CRLF", "CR", "mixed"
}

// ErrBudget is a read beyond Budget; parsers stop there and keep what they
// have.
var ErrBudget = errors.New("meta: byte budget spent")

// budget is an io.ReaderAt that counts what it reads and refuses beyond
// its allowance, so that no format — however its offsets point — makes
// Read take more of a file than Budget.
type budget struct {
	r    io.ReaderAt
	size int64
	left int64

	// FindCover's gate, passed before a picture is copied into memory.
	gate    func() error
	gated   bool
	gateErr error
}

// toMemory passes the gate, once; false when it refused.
func (b *budget) toMemory() bool {
	if b.gate != nil && !b.gated {
		b.gated = true
		b.gateErr = b.gate()
	}
	return b.gateErr == nil
}

func (b *budget) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= b.size {
		return 0, io.EOF
	}
	if int64(len(p)) > b.size-off {
		p = p[:b.size-off]
	}
	if int64(len(p)) > b.left {
		return 0, ErrBudget
	}
	n, err := b.r.ReadAt(p, off)
	b.left -= int64(n)
	if err == nil && n < len(p) {
		err = io.EOF
	}
	return n, err
}

// at reads n bytes at off, or as many as there are; nil when the budget or
// the file gives nothing.
func (b *budget) at(off int64, n int) []byte {
	if n <= 0 || off < 0 || off >= b.size {
		return nil
	}
	if int64(n) > b.size-off {
		n = int(b.size - off)
	}
	buf := make([]byte, n)
	m, _ := b.ReadAt(buf, off)
	return buf[:m]
}

// Read reads what the file r of size bytes says about itself. name helps
// only to tell text from other data; the format comes from the bytes.
func Read(r io.ReaderAt, size int64, name string) Info {
	b := &budget{r: r, size: size, left: Budget}
	h := b.at(0, head)
	info := Info{Kind: KindOther}
	defer func() {
		// A parser's bug on a hostile file must not take the request down;
		// what was read so far stands.
		_ = recover()
	}()
	switch {
	case bytes.HasPrefix(h, []byte{0xFF, 0xD8, 0xFF}):
		info = readJPEG(h)
	case bytes.HasPrefix(h, []byte("\x89PNG\r\n\x1a\n")):
		info = readPNG(h)
	case bytes.HasPrefix(h, []byte("GIF87a")), bytes.HasPrefix(h, []byte("GIF89a")):
		info = readGIF(h)
	case len(h) >= 12 && string(h[:4]) == "RIFF" && string(h[8:12]) == "WEBP":
		info = readWebP(h)
	case len(h) >= 12 && string(h[:4]) == "RIFF" && string(h[8:12]) == "WAVE":
		info = readWAV(b, h)
	case bytes.HasPrefix(h, []byte("BM")) && len(h) >= 26:
		info = readBMP(h)
	case bytes.HasPrefix(h, []byte("II*\x00")), bytes.HasPrefix(h, []byte("MM\x00*")):
		info = Info{Kind: KindImage, Format: "TIFF"}
		applyEXIF(&info, h)
	case len(h) >= 12 && string(h[4:8]) == "ftyp":
		info = readISOBMFF(b)
	case bytes.HasPrefix(h, []byte("fLaC")):
		info = readFLAC(h)
	case bytes.HasPrefix(h, []byte("ID3")), isMPEGFrame(h):
		info = readMP3(b, h)
	case bytes.HasPrefix(h, []byte("%PDF-")):
		info = readPDF(b, h)
	case looksLikeText(h, name):
		info = readText(h, size)
	}
	if info.Kind == "" {
		info.Kind = KindOther
	}
	if info.Orientation >= 5 {
		// Turned a quarter: the picture shown upright is the other way round
		// than the pixels stored.
		info.Width, info.Height = info.Height, info.Width
	}
	return info
}

// clean makes a string from a file fit to show: valid UTF-8, no control
// characters, no surrounding space or padding NULs, and not endless.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' {
			return ' '
		}
		if r < 0x20 || r == 0x7F || (r >= 0x80 && r < 0xA0) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 200 {
		s = string([]rune(s)[:200]) + "…"
	}
	return s
}
