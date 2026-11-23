package docs

import (
	"bytes"
	"reflect"
	"testing"
)

const workbook = `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Haushalt" sheetId="1" r:id="rId7"/><sheet name="Zwei" sheetId="2" r:id="rId8"/></sheets></workbook>`

const workbookRels = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId8" Target="worksheets/sheet2.xml"/><Relationship Id="rId7" Target="worksheets/first.xml"/></Relationships>`

const sharedXML = `<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3">
<si><t>Posten</t></si><si><r><t>Mi</t></r><r><t>ete</t></r><rPh><t>みみ</t></rPh></si><si><t>Betrag</t></si></sst>`

const stylesXML = `<?xml version="1.0"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<numFmts><numFmt numFmtId="164" formatCode="dd/mm/yyyy;@"/><numFmt numFmtId="165" formatCode="#,##0.00 &quot;EUR&quot;"/></numFmts>
<cellXfs><xf numFmtId="0"/><xf numFmtId="164"/><xf numFmtId="165"/><xf numFmtId="22"/></cellXfs></styleSheet>`

const sheetXML = `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>2</v></c></row>
<row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2" s="1"><v>46350</v></c><c r="C2" s="2"><f>SUM(1,2)</f><v>850.5</v></c></row>
<row r="4"><c r="A4" t="inlineStr"><is><t>&lt;b&gt;fett&lt;/b&gt;</t></is></c><c r="B4" t="b"><v>1</v></c><c r="C4" t="e"><v>#DIV/0!</v></c><c r="D4" s="3"><v>46350.5</v></c></row>
<row r="5"><c r="A5" t="s"><v>99</v></c><c r="XFD5"><v>1</v></c></row>
<row r="1048576"><c r="XFD1048576"><v>1</v></c></row>
</sheetData></worksheet>`

func TestReadSheetXLSX(t *testing.T) {
	z := zipOf(t, map[string]string{
		"xl/workbook.xml":            workbook,
		"xl/_rels/workbook.xml.rels": workbookRels,
		"xl/worksheets/first.xml":    sheetXML,
		"xl/worksheets/sheet2.xml":   `<worksheet><sheetData><row><c t="str"><v>nicht hier</v></c></row></sheetData></worksheet>`,
		"xl/sharedStrings.xml":       sharedXML,
		"xl/styles.xml":              stylesXML,
	})
	s, err := ReadSheet(bytes.NewReader(z), int64(len(z)), DefaultSheetLimits)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"Posten", "", "Betrag"},
		{"Miete", "2026-11-24", "850.5"},
		{},
		{"<b>fett</b>", "TRUE", "#DIV/0!", "2026-11-24 12:00:00"},
	}
	if s.Name != "Haushalt" || !reflect.DeepEqual(s.Rows, want) || !s.Truncated {
		t.Errorf("%q %q truncated=%v", s.Name, s.Rows, s.Truncated)
	}
}

func TestReadSheetODS(t *testing.T) {
	ods := `<?xml version="1.0"?><office:document-content xmlns:office="o" xmlns:table="t" xmlns:text="x"><office:body><office:spreadsheet>
<table:table table:name="Tabelle1">
<table:table-row><table:table-cell><text:p>Name</text:p></table:table-cell><table:table-cell table:number-columns-repeated="2"/><table:table-cell><text:p>Ort</text:p></table:table-cell></table:table-row>
<table:table-row table:number-rows-repeated="1048570"><table:table-cell table:number-columns-repeated="16384"/></table:table-row>
<table:table-row><table:table-cell><text:p>Alex</text:p><office:annotation><text:p>Notiz</text:p></office:annotation></table:table-cell><table:table-cell table:number-columns-repeated="3"><text:p>x<text:s text:c="2"/>y</text:p><text:p>z</text:p></table:table-cell></table:table-row>
<table:table-row table:number-rows-repeated="1048576"><table:table-cell table:number-columns-repeated="16384"/></table:table-row>
</table:table><table:table table:name="Zwei"><table:table-row><table:table-cell><text:p>nicht hier</text:p></table:table-cell></table:table-row></table:table>
</office:spreadsheet></office:body></office:document-content>`
	z := zipOf(t, map[string]string{"content.xml": ods})
	lim := DefaultSheetLimits
	lim.Rows = 1048575 // the empty run must cost a count, not a layout
	s, err := ReadSheet(bytes.NewReader(z), int64(len(z)), lim)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Tabelle1" || len(s.Rows) != 1048572 || s.Truncated {
		t.Fatalf("%q %d rows truncated=%v", s.Name, len(s.Rows), s.Truncated)
	}
	if got := s.Rows[0]; !reflect.DeepEqual(got, []string{"Name", "", "", "Ort"}) {
		t.Errorf("row 1: %q", got)
	}
	if got := s.Rows[1048571]; !reflect.DeepEqual(got, []string{"Alex", "x  y\nz", "x  y\nz", "x  y\nz"}) {
		t.Errorf("last row: %q", got)
	}
	// With the default bounds the rows past 1000 are cut off, and said to be.
	s, _ = ReadSheet(bytes.NewReader(z), int64(len(z)), DefaultSheetLimits)
	if len(s.Rows) != 1 || !s.Truncated {
		t.Errorf("bounded: %d rows truncated=%v", len(s.Rows), s.Truncated)
	}
}

func TestReadSheetRepeatedValues(t *testing.T) {
	// A value repeated over every column of every row: laid out only as far
	// as the bounds.
	ods := `<office:document-content><office:body><office:spreadsheet><table:table table:name="T">
<table:table-row table:number-rows-repeated="1048576"><table:table-cell table:number-columns-repeated="16384"><text:p>1</text:p></table:table-cell></table:table-row>
</table:table></office:spreadsheet></office:body></office:document-content>`
	z := zipOf(t, map[string]string{"content.xml": ods})
	lim := SheetLimits{Rows: 50, Cols: 10, Cells: 200, Cell: 16}
	s, err := ReadSheet(bytes.NewReader(z), int64(len(z)), lim)
	if err != nil || !s.Truncated || len(s.Rows) != 20 || len(s.Rows[0]) != 10 {
		t.Fatalf("%d rows, err %v, truncated %v", len(s.Rows), err, s.Truncated)
	}
}

func TestReadSheetNone(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"a text":        {"content.xml": odt},
		"a Word file":   {"word/document.xml": docx},
		"no sheets":     {"xl/workbook.xml": `<workbook><sheets/></workbook>`},
		"sheet missing": {"xl/workbook.xml": workbook},
	} {
		z := zipOf(t, files)
		if _, err := ReadSheet(bytes.NewReader(z), int64(len(z)), DefaultSheetLimits); err != ErrNone {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestFormatKind(t *testing.T) {
	for code, want := range map[string]dateKind{
		"dd.mm.yyyy":           dateOnly,
		"d. mmmm yyyy;@":       dateOnly,
		"[$-407]dddd, d. mmmm": dateOnly,
		"mmm-yy":               dateOnly,
		"hh:mm":                timeOnly,
		"[h]:mm:ss":            timeOnly,
		"mm:ss.0":              timeOnly,
		"yyyy-mm-dd hh:mm":     dateTime,
		"General":              notDate,
		"0.00":                 notDate,
		`#,##0.00 "days"`:      notDate,
		`0.00\d`:               notDate,
		"[Red]0.00":            notDate,
		"0.00E+00":             notDate,
		"#,##0 €":              notDate,
	} {
		if got := formatKind(code); got != want {
			t.Errorf("%q: %v, want %v", code, got, want)
		}
	}
}

func TestSerialDate(t *testing.T) {
	for _, c := range []struct {
		v    string
		k    dateKind
		b    bool
		want string
	}{
		{"1", dateOnly, false, "1900-01-01"},
		{"59", dateOnly, false, "1900-02-28"},
		{"61", dateOnly, false, "1900-03-01"},
		{"46350", dateOnly, false, "2026-11-24"},
		{"46350.75", dateTime, false, "2026-11-24 18:00:00"},
		{"0.5", timeOnly, false, "12:00:00"},
		{"0", dateOnly, true, "1904-01-01"},
		{"44888", dateOnly, true, "2026-11-24"},
	} {
		if got, ok := serialDate(c.v, c.k, c.b); !ok || got != c.want {
			t.Errorf("%s: %q %v, want %q", c.v, got, ok, c.want)
		}
	}
	for _, v := range []string{"60", "-1", "x", "3000000", "NaN"} {
		if got, ok := serialDate(v, dateOnly, false); ok {
			t.Errorf("%s: %q", v, got)
		}
	}
}

func FuzzSheet(f *testing.F) {
	f.Add(sheetXML, sharedXML, `<sheets/>`, "")
	f.Add(`<sheetData><row r="1"><c r="A1" t="s"><v>4194303</v></c><c r="B1" t="s"><v>-5</v></c></row></sheetData>`, sharedXML, "", "")
	f.Add("", "", "",
		`<office:spreadsheet><table:table table:name="T"><table:table-row table:number-rows-repeated="1048576"><table:table-cell table:number-columns-repeated="16384"><text:p>1</text:p></table:table-cell></table:table-row></table:table></office:spreadsheet>`)
	f.Add("", "", "",
		`<office:spreadsheet><table:table><table:table-row><table:table-cell table:number-columns-repeated="99999999999999999999"/><table:table-cell><text:p>x</text:p></table:table-cell></table:table-row></table:table></office:spreadsheet>`)
	lim := SheetLimits{Rows: 40, Cols: 12, Cells: 100, Cell: 32}
	f.Fuzz(func(t *testing.T, sheet, shared, styles, ods string) {
		check := func(s Sheet) {
			if len(s.Rows) > lim.Rows {
				t.Fatalf("%d rows", len(s.Rows))
			}
			cells := 0
			for _, r := range s.Rows {
				if len(r) > lim.Cols {
					t.Fatalf("%d columns", len(r))
				}
				for _, v := range r {
					if len(v) > lim.Cell*3 { // U+FFFD for a byte cut off
						t.Fatalf("%d bytes in a cell", len(v))
					}
					if v != "" {
						cells++
					}
				}
			}
			if cells > lim.Cells {
				t.Fatalf("%d cells", cells)
			}
		}
		x := zipOf(t, map[string]string{
			"xl/workbook.xml":          `<workbook><sheets><sheet name="S"/></sheets></workbook>`,
			"xl/worksheets/sheet1.xml": sheet,
			"xl/sharedStrings.xml":     shared,
			"xl/styles.xml":            styles,
		})
		if s, err := ReadSheet(bytes.NewReader(x), int64(len(x)), lim); err == nil {
			check(s)
		}
		o := zipOf(t, map[string]string{"content.xml": ods})
		if s, err := ReadSheet(bytes.NewReader(o), int64(len(o)), lim); err == nil {
			check(s)
		}
	})
}
