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
	"sort"
	"strconv"
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

// Text returns the plain text of a Word document (word/document.xml), a
// PowerPoint presentation (its slides in order) or an OpenDocument text or
// presentation (content.xml): paragraphs as lines, slides apart by an empty
// line, at most max bytes, and whether there was more.
func Text(r io.ReaderAt, size int64, max int) (string, bool, error) {
	es, err := zipread.List(r, size, maxEntries)
	if err != nil {
		return "", false, err
	}
	if slides := pptxSlides(r, size, es); len(slides) > 0 {
		return slideText(r, size, slides, max)
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
	aside := 0      // OpenDocument: inside a comment or a slide's notes
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
			case !word && (t.Name.Local == "notes" || t.Name.Local == "annotation"):
				aside++
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
			case !word && (t.Name.Local == "notes" || t.Name.Local == "annotation"):
				aside--
			case !word && inBody && (t.Name.Local == "p" || t.Name.Local == "h"):
				inPara--
				if aside == 0 {
					write("\n")
				}
			case !word && inBody && t.Name.Local == "page":
				// A presentation's slide: an empty line before the next.
				write("\n")
			}
		case xml.CharData:
			if (word && inText) || (!word && inPara > 0 && aside == 0) {
				write(string(t))
			}
		}
	}
	return strings.ToValidUTF8(strings.TrimRight(out.String(), "\n"), "") + "\n", full, nil
}

// maxSlides bounds the slides read for a presentation's text.
const maxSlides = 500

// pptxSlides are a PowerPoint file's slides in the order they are shown:
// presentation.xml's list of slides, each found through its relationship;
// a hidden slide (show="0") is left out, as a slideshow leaves it out.
// Without a usable list — the part missing or broken — the slides go in
// their files' numbers' order, slide1.xml, slide2.xml …, which is the
// order until slides were moved.
func pptxSlides(r io.ReaderAt, size int64, es []zipread.Entry) []zipread.Entry {
	if ordered, ok := pptxOrder(r, size, es); ok {
		return ordered
	}
	return pptxNumbered(es)
}

func pptxOrder(r io.ReaderAt, size int64, es []zipread.Entry) ([]zipread.Entry, bool) {
	e, ok := zipread.Find(es, "ppt/presentation.xml")
	if !ok {
		return nil, false
	}
	b, err := zipread.ReadAll(r, size, e, maxOPF)
	if err != nil {
		return nil, false
	}
	var pres struct {
		Slides []struct {
			// r:id, by its local name: the prefix varies.
			Attrs []xml.Attr `xml:",any,attr"`
		} `xml:"sldIdLst>sldId"`
	}
	if xml.Unmarshal(b, &pres) != nil || len(pres.Slides) == 0 {
		return nil, false
	}
	targets := relTargets(r, size, es, "ppt/_rels/presentation.xml.rels", "ppt")
	if targets == nil {
		return nil, false
	}
	var out []zipread.Entry
	for _, sl := range pres.Slides {
		if len(out) == maxSlides {
			break
		}
		for _, a := range sl.Attrs {
			if a.Name.Local != "id" || a.Name.Space == "" {
				continue // the slide's own numeric id, not its relationship
			}
			if t, ok := targets[a.Value]; ok {
				if se, ok := zipread.Find(es, t); ok && !hiddenSlide(r, size, se) {
					out = append(out, se)
				}
			}
		}
	}
	return out, len(out) > 0
}

// hiddenSlide says whether a slide is hidden: show="0" on its root.
func hiddenSlide(r io.ReaderAt, size int64, e zipread.Entry) bool {
	rd, err := zipread.Open(r, size, e, maxXML)
	if err != nil {
		return false
	}
	d := xml.NewDecoder(rd)
	d.Strict = false
	for {
		tok, err := d.Token()
		if err != nil {
			return false
		}
		if st, ok := tok.(xml.StartElement); ok {
			return attr(st, "show") == "0"
		}
	}
}

func pptxNumbered(es []zipread.Entry) []zipread.Entry {
	type numbered struct {
		n int
		e zipread.Entry
	}
	var found []numbered
	for _, e := range es {
		rest, ok := strings.CutPrefix(e.Name, "ppt/slides/slide")
		if !ok {
			continue
		}
		num, ok := strings.CutSuffix(rest, ".xml")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil || n < 1 {
			continue
		}
		found = append(found, numbered{n, e})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].n < found[j].n })
	out := make([]zipread.Entry, 0, min(len(found), maxSlides))
	for i, f := range found {
		if i == maxSlides {
			break
		}
		out = append(out, f.e)
	}
	return out
}

// slideText is the text of a presentation's slides: each slide's
// paragraphs (<a:p>, their <a:t> runs) as lines, an empty line between
// slides.
func slideText(r io.ReaderAt, size int64, slides []zipread.Entry, max int) (string, bool, error) {
	var out strings.Builder
	for i, e := range slides {
		rd, err := zipread.Open(r, size, e, maxXML)
		if err != nil {
			continue // one slide that cannot be read is left out
		}
		text, more, err := slide(rd, max-out.Len())
		if err != nil && text == "" {
			continue
		}
		if i > 0 && out.Len() > 0 && text != "" {
			out.WriteString("\n")
		}
		out.WriteString(text)
		if more || out.Len() >= max {
			return strings.ToValidUTF8(out.String(), ""), true, nil
		}
	}
	if out.Len() == 0 {
		return "", false, ErrNone
	}
	return strings.ToValidUTF8(out.String(), ""), false, nil
}

func slide(r io.Reader, limit int) (string, bool, error) {
	d := xml.NewDecoder(r)
	d.Strict = false
	var out, para strings.Builder
	inT := false
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out.String(), false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inT = true
			case "br":
				para.WriteString("\n")
			case "p":
				para.Reset()
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "p":
				if line := strings.TrimSpace(para.String()); line != "" {
					if out.Len()+len(line)+1 > limit {
						out.WriteString(line[:max(0, min(len(line), limit-out.Len()))])
						return out.String(), true, nil
					}
					out.WriteString(line + "\n")
				}
				para.Reset()
			}
		case xml.CharData:
			if inT && para.Len() < 1<<16 {
				para.Write(t)
			}
		}
	}
	return out.String(), false, nil
}
