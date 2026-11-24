// Package epub reads an EPUB book for reading in the preview: its chapters
// in reading order, and each chapter as a tree of a few known elements and
// plain text. A chapter is XHTML; nothing of it is passed on as markup.
// What a reader needs is kept — paragraphs, headings, emphasis, lists,
// quotes, tables, pictures — and everything else is left out: scripts,
// styles, forms, frames, links (their text stays), attributes. A picture is
// passed on only as the name of an entry of the same archive, checked to be
// there and to be a picture, for the interface to show from the archive.
package epub

import (
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/denisneuling/ferry/internal/zipread"
)

var (
	// ErrNotBook is a file this package reads no book from.
	ErrNotBook = errors.New("epub: not a book")
	// ErrEncrypted is a book whose content is encrypted: DRM. Fonts
	// obfuscated as EPUB and Adobe allow are not that — fonts are left out
	// anyway.
	ErrEncrypted = errors.New("epub: the book is encrypted")
)

// fontObfuscation are the algorithms that only hide embedded fonts.
var fontObfuscation = map[string]bool{
	"http://www.idpf.org/2008/embedding": true,
	"http://ns.adobe.com/pdf/enc#RC":     true,
}

// Bounds.
const (
	maxEntries  = 20000
	maxOPF      = 2 << 20 // container, package and table of contents
	maxChapter  = 4 << 20 // a chapter's XHTML
	maxChapters = 2000
	maxNodes    = 50000
	maxDepth    = 64
	maxText     = 2 << 20
)

// Book is what the reader lists.
type Book struct {
	Title    string
	Chapters []Chapter
}

// Chapter is one document of the reading order: its entry in the archive,
// and its title where the table of contents gives one.
type Chapter struct {
	Entry string
	Title string
}

// Node is a chapter's tree: an element of Tags with its children, or text.
type Node struct {
	Tag      string `json:"tag,omitempty"`
	Text     string `json:"text,omitempty"`
	Children []Node `json:"children,omitempty"`
	// Src: an img's picture, an entry of the archive. Alt: its description.
	Src string `json:"src,omitempty"`
	Alt string `json:"alt,omitempty"`
}

// Tags are the elements a chapter keeps; others give their children only.
var Tags = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"em": true, "strong": true, "u": true, "s": true, "sub": true, "sup": true, "small": true,
	"blockquote": true, "ul": true, "ol": true, "li": true, "br": true, "hr": true,
	"pre": true, "code": true, "table": true, "thead": true, "tbody": true, "tr": true,
	"th": true, "td": true, "figure": true, "figcaption": true, "img": true, "dl": true,
	"dt": true, "dd": true,
}

// renamed are elements kept under another, plainer name.
var renamed = map[string]string{"i": "em", "b": "strong", "cite": "em", "dfn": "em", "del": "s", "strike": "s", "ins": "u", "tt": "code", "kbd": "code", "samp": "code", "var": "em"}

// dropped are elements left out with everything in them.
var dropped = map[string]bool{
	"script": true, "style": true, "head": true, "title": true, "iframe": true, "object": true,
	"embed": true, "form": true, "input": true, "button": true, "select": true, "textarea": true,
	"svg": true, "math": true, "audio": true, "video": true, "canvas": true, "template": true,
	"noscript": true, "nav": false,
}

// blocks are elements that stand on lines of their own: kept as paragraphs'
// boundaries where they are not kept themselves.
var blocks = map[string]bool{"div": true, "section": true, "article": true, "aside": true, "header": true, "footer": true, "main": true, "nav": true, "body": true}

var pictures = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".avif": true, ".bmp": true}

// Open reads a book's reading order: META-INF/container.xml names the
// package; its spine lists the chapters, its manifest where they are.
func Open(r io.ReaderAt, size int64) (Book, []zipread.Entry, error) {
	es, err := zipread.List(r, size, maxEntries)
	if err != nil {
		return Book{}, nil, err
	}
	c, ok := zipread.Find(es, "META-INF/container.xml")
	if !ok {
		return Book{}, nil, ErrNotBook
	}
	b, err := zipread.ReadAll(r, size, c, maxOPF)
	if err != nil {
		return Book{}, nil, err
	}
	var container struct {
		Rootfiles []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if xml.Unmarshal(b, &container) != nil || len(container.Rootfiles) == 0 {
		return Book{}, nil, ErrNotBook
	}
	if encrypted(r, size, es) {
		return Book{}, nil, ErrEncrypted
	}
	opfPath := path.Clean(container.Rootfiles[0].Path)
	opf, ok := zipread.Find(es, opfPath)
	if !ok {
		return Book{}, nil, ErrNotBook
	}
	if b, err = zipread.ReadAll(r, size, opf, maxOPF); err != nil {
		return Book{}, nil, err
	}
	var pkg struct {
		Title []string `xml:"metadata>title"`
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			Type       string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"manifest>item"`
		Spine struct {
			Toc   string `xml:"toc,attr"`
			Items []struct {
				IDRef string `xml:"idref,attr"`
			} `xml:"itemref"`
		} `xml:"spine"`
	}
	if xml.Unmarshal(b, &pkg) != nil {
		return Book{}, nil, ErrNotBook
	}
	dir := path.Dir(opfPath)
	href := map[string]string{}
	navEntry, ncxEntry := "", ""
	for _, it := range pkg.Items {
		name := resolve(dir, it.Href)
		href[it.ID] = name
		if strings.Contains(" "+it.Properties+" ", " nav ") {
			navEntry = name
		}
		if it.ID == pkg.Spine.Toc || it.Type == "application/x-dtbncx+xml" {
			ncxEntry = name
		}
	}
	var book Book
	if len(pkg.Title) > 0 {
		book.Title = clip(strings.TrimSpace(pkg.Title[0]), 1024)
	}
	titles := tocTitles(r, size, es, navEntry, ncxEntry)
	for _, ref := range pkg.Spine.Items {
		name, ok := href[ref.IDRef]
		if !ok {
			continue
		}
		if _, ok := zipread.Find(es, name); !ok {
			continue
		}
		book.Chapters = append(book.Chapters, Chapter{Entry: name, Title: titles[name]})
		if len(book.Chapters) == maxChapters {
			break
		}
	}
	if len(book.Chapters) == 0 {
		return Book{}, nil, ErrNotBook
	}
	return book, es, nil
}

// encrypted says whether META-INF/encryption.xml encrypts anything but
// fonts.
func encrypted(r io.ReaderAt, size int64, es []zipread.Entry) bool {
	e, ok := zipread.Find(es, "META-INF/encryption.xml")
	if !ok {
		return false
	}
	b, err := zipread.ReadAll(r, size, e, maxOPF)
	if err != nil {
		return true // there, and unreadable: not to be read as a book
	}
	var doc struct {
		Data []struct {
			Method struct {
				Algorithm string `xml:"Algorithm,attr"`
			} `xml:"EncryptionMethod"`
		} `xml:"EncryptedData"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return true
	}
	for _, d := range doc.Data {
		if !fontObfuscation[d.Method.Algorithm] {
			return true
		}
	}
	return false
}

// resolve is an href of a part in dir as an entry name: the fragment and
// query dropped, percent-decoded, joined and cleaned; "" when it would leave
// the archive's root or is no path at all. It names an entry only to be
// looked for among the archive's names; it is never a file path.
func resolve(dir, href string) string {
	if i := strings.IndexAny(href, "#?"); i >= 0 {
		href = href[:i]
	}
	if href == "" || strings.Contains(href, ":") {
		return "" // empty, or another scheme: data:, http: …
	}
	if u, err := url.PathUnescape(href); err == nil {
		href = u
	}
	p := path.Clean(path.Join(dir, href))
	if strings.HasPrefix(p, "/") || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// tocTitles are the chapters' titles from the book's table of contents:
// EPUB 3's navigation document, or EPUB 2's NCX.
func tocTitles(r io.ReaderAt, size int64, es []zipread.Entry, nav, ncx string) map[string]string {
	out := map[string]string{}
	read := func(name string) []byte {
		e, ok := zipread.Find(es, name)
		if !ok {
			return nil
		}
		b, err := zipread.ReadAll(r, size, e, maxOPF)
		if err != nil {
			return nil
		}
		return b
	}
	if b := read(ncx); b != nil {
		var doc struct {
			Points []struct {
				Label   string `xml:"navLabel>text"`
				Content struct {
					Src string `xml:"src,attr"`
				} `xml:"content"`
			} `xml:"navMap>navPoint"`
		}
		if xml.Unmarshal(b, &doc) == nil {
			for _, p := range doc.Points {
				name := resolve(path.Dir(ncx), p.Content.Src)
				if _, ok := out[name]; !ok {
					out[name] = clip(strings.TrimSpace(p.Label), 512)
				}
			}
		}
	}
	if b := read(nav); b != nil {
		// The navigation document's links, in order: an <a href> and its text.
		d := xml.NewDecoder(strings.NewReader(string(b)))
		d.Strict, d.AutoClose, d.Entity = false, xml.HTMLAutoClose, xml.HTMLEntity
		var cur string
		var text strings.Builder
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "a" {
					cur = resolve(path.Dir(nav), attr(t, "href"))
					text.Reset()
				}
			case xml.CharData:
				if cur != "" {
					text.Write(t)
				}
			case xml.EndElement:
				if t.Name.Local == "a" && cur != "" {
					if _, ok := out[cur]; !ok {
						out[cur] = clip(strings.Join(strings.Fields(text.String()), " "), 512)
					}
					cur = ""
				}
			}
		}
	}
	return out
}

// ReadChapter reads the chapter entry as a tree. The entry must be one of
// the book's chapters; truncated says a bound cut it short.
func ReadChapter(r io.ReaderAt, size int64, es []zipread.Entry, book Book, entry string) ([]Node, bool, error) {
	ok := false
	for _, c := range book.Chapters {
		if c.Entry == entry {
			ok = true
		}
	}
	e, found := zipread.Find(es, entry)
	if !ok || !found {
		return nil, false, ErrNotBook
	}
	rd, err := zipread.Open(r, size, e, maxChapter)
	if err != nil {
		return nil, false, err
	}
	names := make(map[string]bool, len(es))
	for _, x := range es {
		names[x.Name] = true
	}
	return parse(rd, path.Dir(entry), names)
}

// parse walks a chapter's XHTML and builds its tree.
func parse(r io.Reader, dir string, names map[string]bool) ([]Node, bool, error) {
	d := xml.NewDecoder(r)
	d.Strict, d.AutoClose, d.Entity = false, xml.HTMLAutoClose, xml.HTMLEntity
	root := &Node{}
	stack := []*Node{root}
	// open holds, for each element started, whether it pushed a node: kept
	// elements push, others do not, and their end must not pop.
	var open []bool
	skip := 0 // inside a dropped element, how deep
	nodes, text := 0, 0
	more := false
	inBody := false
	add := func(n Node) *Node {
		top := stack[len(stack)-1]
		top.Children = append(top.Children, n)
		nodes++
		return &top.Children[len(top.Children)-1]
	}
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if nodes > 0 {
				more = true
				break
			}
			return nil, false, ErrNotBook
		}
		if nodes >= maxNodes || text >= maxText {
			more = true
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if name == "body" {
				inBody = true
			}
			if skip > 0 || dropped[name] || !inBody {
				if skip > 0 || dropped[name] {
					skip++
				}
				open = append(open, false)
				continue
			}
			if n, ok := renamed[name]; ok {
				name = n
			}
			if name == "img" || (name == "image" && t.Name.Space != "") {
				src := attr(t, "src")
				if src == "" {
					src = attr(t, "href") // svg <image xlink:href>, though svg itself is left out
				}
				ent := resolve(dir, src)
				alt := clip(attr(t, "alt"), 512)
				if ent != "" && names[ent] && pictures[strings.ToLower(path.Ext(ent))] {
					add(Node{Tag: "img", Src: ent, Alt: alt})
				} else if strings.TrimSpace(alt) != "" {
					add(Node{Text: alt}) // the picture is not there: its description is
				}
				open = append(open, false)
				continue
			}
			if Tags[name] && len(stack) <= maxDepth {
				stack = append(stack, add(Node{Tag: name}))
				open = append(open, true)
				continue
			}
			if blocks[name] && len(stack) <= maxDepth {
				// A block that is not kept still parts what is around it.
				stack = append(stack, add(Node{Tag: "div"}))
				open = append(open, true)
				continue
			}
			open = append(open, false)
		case xml.EndElement:
			if len(open) == 0 {
				continue
			}
			pushed := open[len(open)-1]
			open = open[:len(open)-1]
			if skip > 0 {
				skip--
				continue
			}
			if pushed && len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if skip > 0 || !inBody {
				continue
			}
			s := string(t)
			if strings.TrimSpace(s) == "" && !insidePre(stack) {
				if len(s) > 0 {
					s = " "
				} else {
					continue
				}
			}
			s = strings.ToValidUTF8(s, "�")
			text += len(s)
			top := stack[len(stack)-1]
			if k := len(top.Children); k > 0 && top.Children[k-1].Tag == "" {
				top.Children[k-1].Text += s
			} else {
				add(Node{Text: s})
			}
		}
	}
	return tidy(root.Children), more, nil
}

func insidePre(stack []*Node) bool {
	for _, n := range stack {
		if n.Tag == "pre" {
			return true
		}
	}
	return false
}

// tidy drops empty divisions and text that is only space at a block's edge.
func tidy(ns []Node) []Node {
	out := ns[:0]
	for _, n := range ns {
		if n.Tag != "" {
			n.Children = tidy(n.Children)
			if n.Tag == "div" && len(n.Children) == 0 {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func attr(t xml.StartElement, local string) string {
	for _, a := range t.Attr {
		if strings.EqualFold(a.Name.Local, local) {
			return a.Value
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
