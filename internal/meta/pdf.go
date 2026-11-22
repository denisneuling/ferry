package meta

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// pdfTail is how much of a PDF's end is read: the trailer, the last
// objects, and often the document's info and page tree.
const pdfTail = 512 << 10

var (
	rePagesCount = regexp.MustCompile(`/Type\s*/Pages\b[^>]*?/Count\s+(\d+)|/Count\s+(\d+)[^>]*?/Type\s*/Pages\b`)
	reObjStm     = regexp.MustCompile(`/Type\s*/ObjStm\b`)
	reStream     = regexp.MustCompile(`stream\r?\n`)
)

// readPDF reads a PDF's page count, title, author and producer from its
// start and its end — and from compressed object streams found there —
// without parsing the whole file: a PDF's index lies at its end, and a
// modern one keeps its objects compressed.
func readPDF(b *budget, h []byte) Info {
	info := Info{Kind: KindPDF, Format: "PDF"}
	if v := h[5:min(len(h), 8)]; len(v) == 3 && v[1] == '.' {
		info.Format = "PDF " + string(v)
	}
	text := h
	if b.size > int64(len(h)) {
		tailStart := max(b.size-pdfTail, int64(len(h)))
		text = append(append([]byte{}, h...), b.at(tailStart, int(b.size-tailStart))...)
	}
	text = append(text, inflateObjectStreams(text)...)

	// The root of the page tree has the largest count; inner nodes count
	// only their own pages.
	for _, m := range rePagesCount.FindAllSubmatch(text, 256) {
		s := m[1]
		if len(s) == 0 {
			s = m[2]
		}
		if n, err := strconv.Atoi(string(s)); err == nil && n > info.Pages && n < 1e7 {
			info.Pages = n
		}
	}
	info.Title = pdfField(text, "Title")
	info.Author = pdfField(text, "Author")
	info.Producer = pdfField(text, "Producer")
	if info.Producer == "" {
		info.Producer = pdfField(text, "Creator")
	}
	return info
}

// inflateObjectStreams unpacks the object streams in b that are whole
// within it, at most 2 MiB of them altogether.
func inflateObjectStreams(b []byte) []byte {
	var out []byte
	for _, loc := range reObjStm.FindAllIndex(b, 64) {
		rest := b[loc[1]:]
		s := reStream.FindIndex(rest)
		if s == nil || s[0] > 512 {
			continue
		}
		data := rest[s[1]:]
		end := bytes.Index(data, []byte("endstream"))
		if end < 0 {
			continue
		}
		z, err := zlib.NewReader(bytes.NewReader(data[:end]))
		if err != nil {
			continue
		}
		left := int64(2<<20 - len(out))
		if left <= 0 {
			break
		}
		got, _ := io.ReadAll(io.LimitReader(z, left))
		out = append(out, got...)
		out = append(out, '\n')
	}
	return out
}

// pdfField finds /Name (literal) or /Name <hex> and decodes it; the last
// one wins, since an incremental update appends a newer info dictionary.
func pdfField(b []byte, name string) string {
	key := []byte("/" + name)
	found := ""
	for i := 0; ; {
		j := bytes.Index(b[i:], key)
		if j < 0 {
			break
		}
		p := i + j + len(key)
		i = p
		if p < len(b) && (isPDFName(b[p])) {
			continue // /TitleSomething
		}
		for p < len(b) && (b[p] == ' ' || b[p] == '\r' || b[p] == '\n' || b[p] == '\t') {
			p++
		}
		if p >= len(b) {
			break
		}
		var raw []byte
		switch b[p] {
		case '(':
			raw = pdfLiteral(b[p+1:])
		case '<':
			if p+1 < len(b) && b[p+1] != '<' {
				end := bytes.IndexByte(b[p+1:], '>')
				if end >= 0 && end < 4096 {
					raw = pdfHex(b[p+1 : p+1+end])
				}
			}
		}
		if s := pdfString(raw); s != "" {
			found = s
		}
	}
	return found
}

func isPDFName(c byte) bool {
	return c > ' ' && !strings.ContainsRune("()<>[]{}/%", rune(c))
}

// pdfLiteral reads a literal string up to its closing parenthesis, with
// nesting and escapes, at most 4 KiB.
func pdfLiteral(b []byte) []byte {
	var out []byte
	depth := 0
	for i := 0; i < len(b) && i < 4096; i++ {
		c := b[i]
		switch c {
		case '\\':
			i++
			if i >= len(b) {
				return out
			}
			switch e := b[i]; e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b', 'f':
			case '\r', '\n':
			default:
				if e >= '0' && e <= '7' {
					v, k := 0, 0
					for ; k < 3 && i+k < len(b) && b[i+k] >= '0' && b[i+k] <= '7'; k++ {
						v = v*8 + int(b[i+k]-'0')
					}
					out = append(out, byte(v))
					i += k - 1
				} else {
					out = append(out, e)
				}
			}
		case '(':
			depth++
			out = append(out, c)
		case ')':
			if depth == 0 {
				return out
			}
			depth--
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

func pdfHex(b []byte) []byte {
	var digits []byte
	for _, c := range b {
		if (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'f') {
			digits = append(digits, c)
		}
	}
	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}
	out := make([]byte, len(digits)/2)
	for i := range out {
		v, _ := strconv.ParseUint(string(digits[2*i:2*i+2]), 16, 8)
		out[i] = byte(v)
	}
	return out
}

// pdfString decodes a PDF text string: UTF-16BE with its BOM, or
// PDFDocEncoding, read as Latin-1 — which it matches for letters.
func pdfString(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		}
		return clean(string(utf16.Decode(u)))
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return clean(string(b[3:]))
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return clean(string(r))
}
