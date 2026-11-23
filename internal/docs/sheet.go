package docs

import (
	"encoding/xml"
	"errors"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/denisneuling/ferry/internal/zipread"
)

// SheetLimits bound what a spreadsheet's preview reads and returns.
type SheetLimits struct {
	Rows  int // rows returned, counted from the sheet's first
	Cols  int // columns returned, counted from A
	Cells int // cells with a value, read in all
	Cell  int // bytes of one cell's text
}

// DefaultSheetLimits are the bounds of a preview: a screenful many times
// over, and far less than a sheet can hold.
var DefaultSheetLimits = SheetLimits{Rows: 1000, Cols: 100, Cells: 50000, Cell: 1024}

// Bounds of the parts read for a sheet besides the sheet itself.
const (
	maxSheetXML  = 256 << 20 // a sheet's XML, unpacked; read only until the rows are full
	maxShared    = 64 << 20  // the shared strings, unpacked; read only up to the last one used
	maxStyles    = 4 << 20   // xl/styles.xml
	maxStyleXfs  = 65536     // cell formats read from it
	maxSharedNum = 1 << 22   // shared strings counted through
)

// Sheet is the first sheet of a workbook, as text.
type Sheet struct {
	Name string
	// Rows from the first, each up to its last cell with a value; a row
	// with none is empty. Rows after the last with a value are left out.
	Rows [][]string
	// Truncated: there were more rows, columns or cells than returned.
	Truncated bool
}

// cell is a cell read from a sheet before its rows are laid out.
type cell struct {
	row, col int
	text     string
	shared   int // the shared string it shows, -1 when none
}

// ReadSheet returns the first sheet of an Excel workbook (.xlsx) or an
// OpenDocument spreadsheet (.ods): what each cell shows as it was last
// saved — a formula's stored result, never the formula worked out anew.
func ReadSheet(r io.ReaderAt, size int64, lim SheetLimits) (Sheet, error) {
	es, err := zipread.List(r, size, maxEntries)
	if err != nil {
		return Sheet{}, err
	}
	if _, ok := zipread.Find(es, "xl/workbook.xml"); ok {
		return readXLSX(r, size, es, lim)
	}
	if e, ok := zipread.Find(es, "content.xml"); ok {
		rd, err := openPart(r, size, e, maxSheetXML)
		if err != nil {
			return Sheet{}, err
		}
		return readODS(rd, lim)
	}
	return Sheet{}, ErrNone
}

// readXLSX follows the workbook to its first sheet, reads that sheet's
// cells, then the shared strings they use and the date formats they wear.
func readXLSX(r io.ReaderAt, size int64, es []zipread.Entry, lim SheetLimits) (Sheet, error) {
	wb, _ := zipread.Find(es, "xl/workbook.xml")
	b, err := zipread.ReadAll(r, size, wb, maxOPF)
	if err != nil {
		return Sheet{}, err
	}
	var book struct {
		Pr struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			Name string `xml:"name,attr"`
			// r:id, by its local name: the prefix varies.
			Attrs []xml.Attr `xml:",any,attr"`
		} `xml:"sheets>sheet"`
	}
	if xml.Unmarshal(b, &book) != nil || len(book.Sheets) == 0 {
		return Sheet{}, ErrNone
	}
	first := book.Sheets[0]
	target := "xl/worksheets/sheet1.xml"
	for _, a := range first.Attrs {
		if a.Name.Local == "id" {
			if t, ok := relTarget(r, size, es, a.Value); ok {
				target = t
			}
		}
	}
	e, ok := zipread.Find(es, target)
	if !ok {
		return Sheet{}, ErrNone
	}
	rd, err := openPart(r, size, e, maxSheetXML)
	if err != nil {
		return Sheet{}, err
	}
	cells, styles, more, err := xlsxCells(rd, lim)
	if err != nil {
		return Sheet{}, err
	}
	// Shared strings, only those the cells read use.
	need := map[int]bool{}
	for _, c := range cells {
		if c.shared >= 0 {
			need[c.shared] = true
		}
	}
	strs := map[int]string{}
	if len(need) > 0 {
		if e, ok := zipread.Find(es, "xl/sharedStrings.xml"); ok {
			if rd, err := openPart(r, size, e, maxShared); err == nil {
				strs = sharedStrings(rd, need, lim.Cell)
			}
		}
	}
	var dates map[int]dateKind
	if len(styles) > 0 {
		if e, ok := zipread.Find(es, "xl/styles.xml"); ok {
			if b, err := zipread.ReadAll(r, size, e, maxStyles); err == nil {
				dates = dateStyles(b)
			}
		}
	}
	base1904 := book.Pr.Date1904 == "1" || book.Pr.Date1904 == "true"
	for i := range cells {
		c := &cells[i]
		switch {
		case c.shared >= 0:
			c.text = strs[c.shared] // out of range: empty
		case styles[i] >= 0 && dates[styles[i]] != notDate:
			if s, ok := serialDate(c.text, dates[styles[i]], base1904); ok {
				c.text = s
			}
		}
	}
	return layout(first.Name, cells, more, lim), nil
}

// openPart opens a part that is read as a stream and only as far as
// needed: a large one is not refused for its size, its reading just ends
// after limit bytes, as a broken XML would.
func openPart(r io.ReaderAt, size int64, e zipread.Entry, limit int64) (io.Reader, error) {
	rd, err := zipread.Open(r, size, e, 0)
	if err != nil {
		return nil, err
	}
	return io.LimitReader(rd, limit), nil
}

// relTarget looks a sheet's relationship id up in the workbook's
// relationships; the target is only ever a name looked for in the archive.
func relTarget(r io.ReaderAt, size int64, es []zipread.Entry, id string) (string, bool) {
	e, ok := zipread.Find(es, "xl/_rels/workbook.xml.rels")
	if !ok {
		return "", false
	}
	b, err := zipread.ReadAll(r, size, e, maxOPF)
	if err != nil {
		return "", false
	}
	var rels struct {
		Rel []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(b, &rels) != nil {
		return "", false
	}
	for _, x := range rels.Rel {
		if x.ID != id {
			continue
		}
		if strings.HasPrefix(x.Target, "/") {
			return path.Clean(x.Target[1:]), true
		}
		return path.Clean(path.Join("xl", x.Target)), true
	}
	return "", false
}

// xlsxCells reads a worksheet's cells with a value, in order: their place,
// their text or the shared string they show, and their style (-1 for
// none, and for any cell that is not a plain number). Rows and columns
// past the bounds are skipped, never laid out, so a cell at XFD1048576
// costs nothing.
func xlsxCells(r io.Reader, lim SheetLimits) (cells []cell, styles []int, more bool, err error) {
	d := xml.NewDecoder(r)
	d.Strict = false
	row, col := 0, 0
	var cur *cell
	style, typ := -1, ""
	var text strings.Builder
	inV, inT, inIs, inF := false, false, false, false
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return cells, styles, more, nil
		}
		if err != nil {
			// What was read is still the sheet up to there.
			if len(cells) > 0 {
				return cells, styles, true, nil
			}
			return nil, nil, false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				if n, ok := attrInt(t, "r"); ok && n > row {
					row = n
				} else {
					row++
				}
				col = 0
				if row > lim.Rows {
					return cells, styles, true, nil
				}
			case "c":
				if ref := attr(t, "r"); ref != "" {
					if c, ok := colOf(ref); ok && c > col {
						col = c
					} else {
						col++
					}
				} else {
					col++
				}
				style, typ = -1, attr(t, "t")
				if s, ok := attrInt(t, "s"); ok && s >= 0 {
					style = s
				}
				text.Reset()
				cur = &cell{row: row, col: col, shared: -1}
			case "v":
				inV = true
			case "is":
				inIs = true
			case "t":
				inT = true
			case "f":
				inF = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "is":
				inIs = false
			case "t":
				inT = false
			case "f":
				inF = false
			case "c":
				if cur == nil {
					break
				}
				c := *cur
				cur = nil
				if c.row < 1 || c.col > lim.Cols {
					more = more || c.col > lim.Cols
					break
				}
				v := text.String()
				st := -1
				switch typ {
				case "s":
					n, err := strconv.Atoi(strings.TrimSpace(v))
					if err != nil || n < 0 || n >= maxSharedNum {
						continue
					}
					c.shared = n
				case "b":
					c.text = map[string]string{"1": "TRUE", "0": "FALSE"}[strings.TrimSpace(v)]
				case "", "n":
					c.text = strings.TrimSpace(v)
					st = style
				default: // str, inlineStr, e: text as it stands
					c.text = v
				}
				if c.shared < 0 && c.text == "" {
					break
				}
				if len(cells) == lim.Cells {
					return cells, styles, true, nil
				}
				cells = append(cells, c)
				styles = append(styles, st)
			}
		case xml.CharData:
			// The value, or an inline string's text; a formula is not shown.
			if cur != nil && !inF && (inV || (inIs && inT)) && text.Len() < lim.Cell {
				text.Write(cut(t, lim.Cell-text.Len()))
			}
		}
	}
}

// sharedStrings streams the shared-string table and keeps those in need,
// stopping after the last of them: a table of millions costs only the
// reading up to there.
func sharedStrings(r io.Reader, need map[int]bool, maxCell int) map[int]string {
	last := -1
	for n := range need {
		last = max(last, n)
	}
	out := map[int]string{}
	d := xml.NewDecoder(r)
	d.Strict = false
	i := -1
	inT, inPh := false, 0
	var text strings.Builder
	for i <= last {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				i++
				text.Reset()
			case "t":
				inT = true
			case "rPh": // a phonetic reading, not the text
				inPh++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				if need[i] {
					out[i] = text.String()
				}
			case "t":
				inT = false
			case "rPh":
				inPh--
			}
		case xml.CharData:
			if inT && inPh == 0 && need[i] && text.Len() < maxCell {
				text.Write(cut(t, maxCell-text.Len()))
			}
		}
	}
	return out
}

// dateKind is how a number formatted as a date is shown.
type dateKind int

const (
	notDate  dateKind = iota
	dateOnly          // 2026-11-24
	dateTime          // 2026-11-24 14:05:00
	timeOnly          // 14:05:00
)

// dateStyles reads which cell formats (by index in cellXfs) show a date or
// a time. The rule: Excel's built-in formats 14–17 and 22 are dates (22
// with the time), 18–21 and 45–47 times; a custom format is a date when,
// outside quoted text, brackets and escapes, it has y, d or m with no h
// or s (a date), or h or s with no y or d (a time), or both (both). A
// format with none of them is a number.
func dateStyles(b []byte) map[int]dateKind {
	var st struct {
		Fmts []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Xfs []struct {
			ID int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if xml.Unmarshal(b, &st) != nil {
		return nil
	}
	custom := map[int]dateKind{}
	for _, f := range st.Fmts {
		custom[f.ID] = formatKind(f.Code)
	}
	out := map[int]dateKind{}
	for i, x := range st.Xfs {
		if i == maxStyleXfs {
			break
		}
		k, ok := custom[x.ID]
		if !ok {
			k = builtinKind(x.ID)
		}
		if k != notDate {
			out[i] = k
		}
	}
	return out
}

func builtinKind(id int) dateKind {
	switch {
	case id >= 14 && id <= 17:
		return dateOnly
	case id == 22:
		return dateTime
	case id >= 18 && id <= 21, id >= 45 && id <= 47:
		return timeOnly
	}
	return notDate
}

// formatKind reads a custom number format by the rule of dateStyles.
func formatKind(code string) dateKind {
	// The first section only: "dd.mm.yyyy;@" is a date.
	var date, clock, inQuote bool
	for i := 0; i < len(code); i++ {
		ch := code[i]
		switch {
		case inQuote:
			inQuote = ch != '"'
		case ch == '"':
			inQuote = true
		case ch == '\\' || ch == '_' || ch == '*':
			i++ // the next character is literal, or padding
		case ch == '[':
			// [h] is elapsed hours, a time; [Red] and [$-407] are not.
			end := strings.IndexByte(code[i:], ']')
			if end < 0 {
				return notDate
			}
			in := strings.ToLower(code[i+1 : i+end])
			if in != "" && strings.Trim(in, "hms") == "" {
				clock = true
			}
			i += end
		case ch == ';':
			i = len(code)
		default:
			switch ch | 0x20 {
			case 'y', 'd':
				date = true
			case 'm':
				// m is the month unless it sits by an h or an s, as in
				// hh:mm or mm:ss; then it is a minute.
				if !nearClock(code, i) {
					date = true
				}
			case 'h', 's':
				clock = true
			case 'e', 'g':
				// General, and E+ in a scientific format: a number.
				return notDate
			}
		}
	}
	switch {
	case date && clock:
		return dateTime
	case date:
		return dateOnly
	case clock:
		return timeOnly
	}
	return notDate
}

// nearClock says whether the m at i is a minute: the closest letter before
// it (past separators) is h, or the closest after it is s.
func nearClock(code string, i int) bool {
	letter := func(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }
	for j := i - 1; j >= 0; j-- {
		if c := code[j] | 0x20; letter(code[j]) && c != 'm' {
			return c == 'h'
		}
	}
	for j := i + 1; j < len(code); j++ {
		if c := code[j] | 0x20; letter(code[j]) && c != 'm' {
			return c == 's'
		}
	}
	return false
}

// serialDate writes an Excel date number as ISO 8601. In the 1900 system
// day 1 is 1900-01-01 and day 60 the 29 February 1900 Excel counts though
// it never was — shown as the number it is; in the 1904 system day 0 is
// 1904-01-01. Seconds are rounded; anything out of 0001–9999 stays a
// number.
func serialDate(v string, k dateKind, base1904 bool) (string, bool) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || f < 0 || f >= 2958466 {
		return "", false
	}
	days := math.Floor(f)
	secs := math.Round((f - days) * 86400)
	var t time.Time
	switch {
	case base1904:
		t = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	case days == 60:
		return "", false
	case days < 60:
		t = time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
	default:
		t = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	}
	t = t.AddDate(0, 0, int(days)).Add(time.Duration(secs) * time.Second)
	switch k {
	case dateOnly:
		return t.Format("2006-01-02"), true
	case timeOnly:
		return t.Format("15:04:05"), true
	default:
		return t.Format("2006-01-02 15:04:05"), true
	}
}

// readODS reads an OpenDocument spreadsheet's first table: what each cell
// shows (its paragraphs), with repeated rows and columns laid out only as
// far as the bounds and only when a cell with a value follows them — a
// table that repeats an empty row a million times costs one count.
func readODS(r io.Reader, lim SheetLimits) (Sheet, error) {
	d := xml.NewDecoder(r)
	d.Strict = false
	var s Sheet
	var cells []cell
	inSheet := false // inside office:spreadsheet, not a text's table
	tables := 0      // the first table, and tables inside it
	depth := 0       // inside the first table: how many tables deep
	row, col := 0, 0
	rowRepeat := 1
	var rowCells []cell
	inCell, inNote, para := false, 0, 0
	cellRepeat := 1
	var text strings.Builder
	full := func() bool { return len(cells) >= lim.Cells }
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if len(cells) > 0 {
				s.Truncated = true
				break
			}
			return Sheet{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "spreadsheet":
				inSheet = true
			case "table":
				if depth > 0 {
					depth++
					break
				}
				if !inSheet {
					break
				}
				tables++
				if tables > 1 {
					return layout(s.Name, cells, s.Truncated, lim), nil
				}
				depth = 1
				s.Name = attr(t, "name")
			case "table-row":
				if depth != 1 {
					break
				}
				rowRepeat = 1
				if n, ok := attrInt(t, "number-rows-repeated"); ok && n > 1 {
					rowRepeat = n
				}
				col = 0
				rowCells = rowCells[:0]
			case "table-cell", "covered-table-cell":
				if depth != 1 {
					break
				}
				inCell = true
				cellRepeat = 1
				if n, ok := attrInt(t, "number-columns-repeated"); ok && n > 1 {
					cellRepeat = n
				}
				text.Reset()
				para = 0
			case "annotation":
				inNote++
			case "p", "h":
				if inCell && inNote == 0 {
					if para > 0 {
						write(&text, "\n", lim.Cell)
					}
					para++
				}
			case "s":
				if inCell && inNote == 0 {
					n, _ := attrInt(t, "c")
					write(&text, strings.Repeat(" ", min(max(n, 1), 64)), lim.Cell)
				}
			case "tab":
				if inCell && inNote == 0 {
					write(&text, "\t", lim.Cell)
				}
			case "line-break":
				if inCell && inNote == 0 {
					write(&text, "\n", lim.Cell)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "table":
				if depth > 1 {
					depth--
					break
				}
				if depth == 1 {
					return layout(s.Name, cells, s.Truncated, lim), nil
				}
			case "annotation":
				inNote--
			case "table-cell", "covered-table-cell":
				if depth != 1 || !inCell {
					break
				}
				inCell = false
				v := text.String()
				// Columns past the bound are not laid out; an empty run
				// only moves the column on.
				first := col + 1
				col += cellRepeat
				if v == "" {
					break
				}
				for c := first; c <= col; c++ {
					if c > lim.Cols {
						s.Truncated = true
						break
					}
					rowCells = append(rowCells, cell{col: c, text: v, shared: -1})
				}
			case "table-row":
				if depth != 1 {
					break
				}
				first := row + 1
				row += rowRepeat
				if len(rowCells) == 0 {
					break
				}
				for rr := first; rr <= row; rr++ {
					if rr > lim.Rows {
						s.Truncated = true
						return layout(s.Name, cells, true, lim), nil
					}
					for _, c := range rowCells {
						if full() {
							return layout(s.Name, cells, true, lim), nil
						}
						c.row = rr
						cells = append(cells, c)
					}
				}
			}
		case xml.CharData:
			if inCell && inNote == 0 && para > 0 {
				write(&text, string(t), lim.Cell)
			}
		}
	}
	if s.Name == "" && cells == nil {
		return Sheet{}, ErrNone
	}
	return layout(s.Name, cells, s.Truncated, lim), nil
}

// layout puts cells in rows: as many rows as up to the last with a value,
// each as long as up to its last cell. Cells are in order of their rows
// and, in a row, of their columns; a later cell at the same place wins.
func layout(name string, cells []cell, more bool, lim SheetLimits) Sheet {
	s := Sheet{Name: name, Truncated: more}
	for _, c := range cells {
		if c.row < 1 || c.row > lim.Rows || c.col < 1 || c.col > lim.Cols {
			continue
		}
		for len(s.Rows) < c.row {
			s.Rows = append(s.Rows, nil)
		}
		r := s.Rows[c.row-1]
		for len(r) < c.col {
			r = append(r, "")
		}
		r[c.col-1] = strings.ToValidUTF8(c.text, "�")
		s.Rows[c.row-1] = r
	}
	// A row that ended with empty cells (a shared string out of range)
	// keeps its length; rows after the last with a value go.
	for len(s.Rows) > 0 && allEmpty(s.Rows[len(s.Rows)-1]) {
		s.Rows = s.Rows[:len(s.Rows)-1]
	}
	for i, r := range s.Rows {
		if r == nil {
			s.Rows[i] = []string{}
		}
	}
	return s
}

func allEmpty(r []string) bool {
	for _, v := range r {
		if v != "" {
			return false
		}
	}
	return true
}

// colOf reads the column of a cell reference, B7 → 2; at most three
// letters, as XFD is the last column there is.
func colOf(ref string) (int, bool) {
	n, i := 0, 0
	for ; i < len(ref) && i < 4; i++ {
		c := ref[i] | 0x20
		if c < 'a' || c > 'z' {
			break
		}
		n = n*26 + int(c-'a'+1)
	}
	if i == 0 || i > 3 {
		return 0, false
	}
	return n, true
}

func attr(t xml.StartElement, local string) string {
	for _, a := range t.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// attrInt reads a whole-number attribute, capped well below overflow: a
// repeat of 10^18 is as much as one of 10^9 to anything bounded.
func attrInt(t xml.StartElement, local string) (int, bool) {
	v := attr(t, local)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return int(min(max(n, -1), 1<<30)), true
}

// cut returns at most n bytes of b.
func cut(b []byte, n int) []byte {
	if n < 0 {
		return nil
	}
	if len(b) > n {
		return b[:n]
	}
	return b
}

func write(b *strings.Builder, s string, maxLen int) {
	if b.Len() < maxLen {
		b.WriteString(string(cut([]byte(s), maxLen-b.Len())))
	}
}
