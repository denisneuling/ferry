// Package docs reads what office documents and e-books carry for a
// preview: the thumbnail an Office or OpenDocument file has written into
// itself, an EPUB's cover, and the plain text of a Word or OpenDocument
// text. All are ZIP files (internal/zipread); nothing here renders a
// document, runs its macros or follows its links. The text is text: no
// markup survives, so nothing of the document runs where it is shown.
package docs

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/denisneuling/ferry/internal/zipread"
)

// ErrNone is a file that carries no thumbnail or text this package reads.
var ErrNone = errors.New("docs: nothing to show")

// Bounds.
const (
	maxEntries = 10000    // entries in a document's ZIP
	maxXML     = 16 << 20 // an XML part, unpacked
	maxOPF     = 1 << 20  // an EPUB's container and package files
)

// IsZip says whether head, a file's first bytes, is a ZIP file.
func IsZip(head []byte) bool { return bytes.HasPrefix(head, []byte("PK\x03\x04")) }

// Thumbnail finds the picture a document carries for its preview: Office's
// docProps/thumbnail (JPEG or PNG), OpenDocument's Thumbnails/thumbnail.png,
// or an EPUB's cover image. It returns the entry, to be read with
// zipread.Open.
func Thumbnail(r io.ReaderAt, size int64) (zipread.Entry, error) {
	es, err := zipread.List(r, size, maxEntries)
	if err != nil {
		return zipread.Entry{}, err
	}
	for _, name := range []string{"docProps/thumbnail.jpeg", "docProps/thumbnail.jpg", "docProps/thumbnail.png", "Thumbnails/thumbnail.png"} {
		if e, ok := zipread.Find(es, name); ok && e.Readable() {
			return e, nil
		}
	}
	if cover, ok := epubCover(r, size, es); ok {
		return cover, nil
	}
	return zipread.Entry{}, ErrNone
}

// epubCover follows an EPUB from its container to its package file to the
// cover: EPUB 3's item with the cover-image property, or EPUB 2's
// <meta name="cover"> naming an item.
func epubCover(r io.ReaderAt, size int64, es []zipread.Entry) (zipread.Entry, bool) {
	c, ok := zipread.Find(es, "META-INF/container.xml")
	if !ok {
		return zipread.Entry{}, false
	}
	b, err := zipread.ReadAll(r, size, c, maxOPF)
	if err != nil {
		return zipread.Entry{}, false
	}
	var container struct {
		Rootfiles []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if xml.Unmarshal(b, &container) != nil || len(container.Rootfiles) == 0 {
		return zipread.Entry{}, false
	}
	opfPath := container.Rootfiles[0].Path
	opf, ok := zipread.Find(es, opfPath)
	if !ok {
		return zipread.Entry{}, false
	}
	if b, err = zipread.ReadAll(r, size, opf, maxOPF); err != nil {
		return zipread.Entry{}, false
	}
	var pkg struct {
		Metas []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"metadata>meta"`
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			Type       string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"manifest>item"`
	}
	if xml.Unmarshal(b, &pkg) != nil {
		return zipread.Entry{}, false
	}
	coverID := ""
	for _, m := range pkg.Metas {
		if m.Name == "cover" {
			coverID = m.Content
		}
	}
	for _, it := range pkg.Items {
		isCover := strings.Contains(" "+it.Properties+" ", " cover-image ") || (coverID != "" && it.ID == coverID)
		if !isCover || !strings.HasPrefix(it.Type, "image/") {
			continue
		}
		// The href is relative to the package file, and only ever looked
		// up among the archive's names: nothing is opened by it.
		name := path.Clean(path.Join(path.Dir(opfPath), it.Href))
		if e, ok := zipread.Find(es, name); ok && e.Readable() {
			return e, true
		}
	}
	return zipread.Entry{}, false
}

// Text returns the plain text of a Word document (word/document.xml) or an
// OpenDocument text (content.xml): paragraphs as lines, at most max bytes,
// and whether there was more.
func Text(r io.ReaderAt, size int64, max int) (string, bool, error) {
	es, err := zipread.List(r, size, maxEntries)
	if err != nil {
		return "", false, err
	}
	var part zipread.Entry
	var word bool
	if e, ok := zipread.Find(es, "word/document.xml"); ok {
		part, word = e, true
	} else if e, ok := zipread.Find(es, "content.xml"); ok {
		part = e
	} else {
		return "", false, ErrNone
	}
	rd, err := zipread.Open(r, size, part, maxXML)
	if err != nil {
		return "", false, err
	}
	return extract(rd, word, max)
}

// extract walks the XML and keeps its text: in Word, <w:t> runs, a tab for
// <w:tab/>, a line for each paragraph and break; in OpenDocument, every
// character in the body, a line for each paragraph and heading, spaces and
// tabs for <text:s/> and <text:tab/>; what lies between paragraphs is
// layout, not text.
func extract(r io.Reader, word bool, max int) (string, bool, error) {
	d := xml.NewDecoder(r)
	d.Strict = false
	var out strings.Builder
	inText := false // Word: inside <w:t>
	inBody := false // OpenDocument: inside <office:body>
	inPara := 0     // OpenDocument: inside a paragraph or heading, how deep
	full := false
	write := func(s string) {
		if full {
			return
		}
		if out.Len()+len(s) > max {
			full = true
			s = s[:max-out.Len()]
		}
		out.WriteString(s)
	}
	for !full {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if out.Len() > 0 {
				break // what was read is still the text up to there
			}
			return "", false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case word && t.Name.Local == "t":
				inText = true
			case word && t.Name.Local == "tab":
				write("\t")
			case word && (t.Name.Local == "br" || t.Name.Local == "cr"):
				write("\n")
			case !word && t.Name.Local == "body":
				inBody = true
			case !word && inBody && (t.Name.Local == "p" || t.Name.Local == "h"):
				inPara++
			case !word && inBody && t.Name.Local == "s":
				write(" ")
			case !word && inBody && t.Name.Local == "tab":
				write("\t")
			case !word && inBody && t.Name.Local == "line-break":
				write("\n")
			}
		case xml.EndElement:
			switch {
			case word && t.Name.Local == "t":
				inText = false
			case word && t.Name.Local == "p":
				write("\n")
			case !word && inBody && (t.Name.Local == "p" || t.Name.Local == "h"):
				inPara--
				write("\n")
			}
		case xml.CharData:
			if (word && inText) || (!word && inPara > 0) {
				write(string(t))
			}
		}
	}
	return strings.ToValidUTF8(strings.TrimRight(out.String(), "\n"), "") + "\n", full, nil
}
