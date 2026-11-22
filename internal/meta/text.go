package meta

import (
	"bytes"
	"path"
	"strings"
	"unicode/utf8"
)

// textNames are the extensions read as text when the bytes allow it.
var textNames = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".json": true,
	".log": true, ".xml": true, ".yaml": true, ".yml": true, ".ini": true, ".conf": true,
	".cfg": true, ".toml": true, ".html": true, ".htm": true, ".css": true, ".js": true,
	".ts": true, ".go": true, ".py": true, ".sh": true, ".sql": true, ".srt": true, ".vtt": true,
	".tex": true, ".rst": true, ".org": true, ".env": true, ".properties": true,
}

// looksLikeText is whether a file is text: a text name, or no NUL bytes in
// its start, with a byte-order mark taken as text either way.
func looksLikeText(h []byte, name string) bool {
	if bytes.HasPrefix(h, []byte{0xEF, 0xBB, 0xBF}) || bytes.HasPrefix(h, []byte{0xFF, 0xFE}) || bytes.HasPrefix(h, []byte{0xFE, 0xFF}) {
		return true
	}
	if len(h) == 0 {
		return textNames[strings.ToLower(path.Ext(name))]
	}
	sample := h[:min(len(h), 8192)]
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	if textNames[strings.ToLower(path.Ext(name))] {
		return true
	}
	// Without a text name, only what is plainly text: valid UTF-8 and
	// hardly any control characters.
	if !utf8.Valid(trimPartialRune(sample)) {
		return false
	}
	ctl := 0
	for _, c := range sample {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' && c != '\f' {
			ctl++
		}
	}
	return ctl*100 < len(sample)
}

// readText tells the encoding and counts lines and their endings in the
// head; a longer file's count is a lower bound.
func readText(h []byte, size int64) Info {
	info := Info{Kind: KindText, Format: "Text"}
	body := h
	switch {
	case bytes.HasPrefix(h, []byte{0xEF, 0xBB, 0xBF}):
		info.Encoding, body = "utf-8-bom", h[3:]
	case bytes.HasPrefix(h, []byte{0xFF, 0xFE}):
		info.Encoding = "utf-16le"
	case bytes.HasPrefix(h, []byte{0xFE, 0xFF}):
		info.Encoding = "utf-16be"
	case isASCII(h):
		info.Encoding = "ascii"
	case utf8.Valid(trimPartialRune(h)):
		info.Encoding = "utf-8"
	default:
		// Not UTF-8: some 8-bit code page, most likely Windows-1252 or
		// ISO-8859-1; which one the bytes cannot tell.
		info.Encoding = "8bit"
	}
	if strings.HasPrefix(info.Encoding, "utf-16") {
		// Lines in UTF-16 are counted by their two-byte newlines.
		nl := []byte{'\n', 0}
		if info.Encoding == "utf-16be" {
			nl = []byte{0, '\n'}
		}
		info.Lines = bytes.Count(h[2:], nl)
	} else {
		lf, crlf := bytes.Count(body, []byte("\n")), bytes.Count(body, []byte("\r\n"))
		cr := bytes.Count(body, []byte("\r")) - crlf
		info.Lines = lf + cr
		switch {
		case lf == 0 && cr == 0:
		case crlf == lf && cr == 0:
			info.LineEndings = "CRLF"
		case crlf == 0 && cr == 0:
			info.LineEndings = "LF"
		case lf == 0:
			info.LineEndings = "CR"
		default:
			info.LineEndings = "mixed"
		}
		if len(body) > 0 && body[len(body)-1] != '\n' && body[len(body)-1] != '\r' {
			info.Lines++ // a last line without its newline
		}
	}
	info.LinesAtLeast = size > int64(len(h))
	return info
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// trimPartialRune drops a rune cut off at the end of a head.
func trimPartialRune(b []byte) []byte {
	for i := 1; i <= 3 && i <= len(b); i++ {
		if r, _ := utf8.DecodeLastRune(b[:len(b)-i+1]); r != utf8.RuneError {
			return b[:len(b)-i+1]
		}
	}
	return b
}
