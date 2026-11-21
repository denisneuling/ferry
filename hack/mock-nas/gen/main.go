// Command gen writes the sample files of the mock NAS (hack/mock-nas): a
// fictional household's shares, with photos, PDFs, audio, text and deep
// folders, under -out. Everything is generated, deterministically: no file
// comes from anywhere, and the names and texts are invented.
//
// The large video is not written here; the container makes it at start
// (a sparse file of zeros), so that the image stays small.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	out := flag.String("out", "seed", "directory to write the shares into")
	flag.Parse()
	g := &gen{root: *out, r: rand.New(rand.NewSource(2026))}
	g.alex()
	g.sam()
	g.family()
	g.public()
	g.scans()
	log.Printf("wrote %d files, %.1f MB, under %s", g.files, float64(g.bytes)/1e6, *out)
}

type gen struct {
	root  string
	r     *rand.Rand
	files int
	bytes int64
}

// The modification times spread over the last two years, so sorting by date
// means something.
var epoch = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

func (g *gen) write(path string, b []byte) {
	full := filepath.Join(g.root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(full, b, 0o644); err != nil {
		log.Fatal(err)
	}
	t := epoch.Add(-time.Duration(g.r.Intn(700*24)) * time.Hour)
	_ = os.Chtimes(full, t, t)
	g.files++
	g.bytes += int64(len(b))
}

func (g *gen) dir(path string) {
	if err := os.MkdirAll(filepath.Join(g.root, filepath.FromSlash(path)), 0o755); err != nil {
		log.Fatal(err)
	}
}

// ── The shares ──────────────────────────────────────────────────────────────

func (g *gen) alex() {
	for i, o := range []int{1, 6, 1, 8, 1, 3, 6, 1, 1, 6, 1, 1, 8, 1, 6, 1, 1, 1, 6, 1, 3, 1, 1, 6} {
		g.write(fmt.Sprintf("alex/Photos/2025 Sommer am See/IMG_%04d.jpg", 2101+i), g.photo(4032, 3024, o))
	}
	for i := 0; i < 12; i++ {
		g.write(fmt.Sprintf("alex/Photos/2024 Herbst/IMG_%04d.jpg", 1301+i), g.photo(3024, 4032, 1))
	}
	g.write("alex/Photos/Screenshots/Bildschirmfoto 2026-03-02.png", g.screenshot(1920, 1080))
	g.write("alex/Photos/Panorama Berge 🏔️.jpg", g.photo(9000, 2000, 1))
	g.write("alex/Documents/Lebenslauf.pdf", pdf("Lebenslauf", 2))
	g.write("alex/Documents/Steuern 2025/Belege/Q1/Januar/Rechnung Fahrrad.pdf", pdf("Rechnung Fahrrad", 1))
	g.write("alex/Documents/Steuern 2025/Belege/Q1/Februar/Rechnung Brille.pdf", pdf("Rechnung Brille", 1))
	g.write("alex/Documents/Steuern 2025/Belege/Q2/April/Handwerker Bad.pdf", pdf("Handwerker Bad", 3))
	g.write("alex/Documents/Steuern 2025/Übersicht.md", []byte(steuern))
	g.write("alex/Documents/Notizen.md", []byte(notizen))
	g.write("alex/Documents/todo.txt", []byte("- Fahrrad zur Inspektion\n- Geschenk für Sam\n- Backup prüfen\n"))
	g.write("alex/Music/Sprachnotiz Einkauf.wav", wav(4, 440))
	// A folder with many entries, for listings, sorting and selection.
	for i := 0; i < 250; i++ {
		d := epoch.AddDate(0, 0, -3*i)
		g.write(fmt.Sprintf("alex/Archiv/Briefe/%s Brief %03d.txt", d.Format("2006-01-02"), i+1),
			[]byte(fmt.Sprintf("Brief %d\n\nLiebe Grüße,\nAlex\n", i+1)))
	}
	g.dir("alex/Videos")
	g.dir("alex/Leerer Ordner")
}

func (g *gen) sam() {
	for i, o := range []int{1, 1, 6, 1, 8, 1, 1, 6, 1, 1} {
		g.write(fmt.Sprintf("sam/Fotos/Konzert 🎸/DSC%05d.jpg", 4410+i), g.photo(6000, 4000, o))
	}
	g.write("sam/Uni/Bachelorarbeit/Entwurf v3.pdf", pdf("Entwurf v3", 12))
	g.write("sam/Uni/Bachelorarbeit/Literatur.md", []byte("# Literatur\n\n- Ein Buch über Häfen\n- Ein Aufsatz über Fähren\n"))
	g.write("sam/Uni/Notizen Seminar.txt", []byte("Woche 3: Wiederholung\nWoche 4: Präsentation vorbereiten\n"))
	g.write("sam/Rezepte privat/Ramen.md", []byte("# Ramen\n\nBrühe acht Stunden, Geduld.\n"))
}

func (g *gen) family() {
	for i, o := range []int{1, 6, 1, 1, 8, 1, 6, 1} {
		g.write(fmt.Sprintf("Family/Fotos/Weihnachten 🎄 2025/IMG_%04d.jpg", 3001+i), g.photo(4032, 3024, o))
	}
	for i := 0; i < 6; i++ {
		g.write(fmt.Sprintf("Family/Fotos/Geburtstag Oma/Foto %d.jpg", i+1), g.photo(2048, 1536, 1))
	}
	g.write("Family/Rezepte/Apfelkuchen.md", []byte(apfelkuchen))
	g.write("Family/Rezepte/Kürbissuppe.md", []byte("# Kürbissuppe\n\n1 Hokkaido, 1 Zwiebel, 500 ml Brühe, Ingwer.\n"))
	g.write("Family/Ärzte & Versicherungen/Impfpass Kopie.pdf", pdf("Impfpass Kopie", 2))
	g.write("Family/Ärzte & Versicherungen/Hausratversicherung 2026.pdf", pdf("Hausratversicherung", 4))
	g.write("Family/Haushalt/Einkaufsliste.txt", []byte("Milch\nBrot\nÄpfel\nKaffee ☕\n"))
	g.write("Family/Haushalt/Nebenkosten 2025.csv", []byte("Monat;Strom;Wasser\nJanuar;61,20;22,10\nFebruar;58,90;21,40\n"))
	g.write("Family/Urlaubsplanung 2027.md", []byte("# Urlaub 2027\n\n- Ostsee oder Alpen?\n- Fähre buchen ⛴️\n"))
}

func (g *gen) public() {
	g.write("Public/Handbücher/Waschmaschine.pdf", pdf("Waschmaschine", 6))
	g.write("Public/Handbücher/Router.pdf", pdf("Router", 8))
	g.write("Public/WLAN für Gäste.txt", []byte("Netz: Gaeste\nDas Passwort steht auf dem Kühlschrank.\n"))
	g.write("Public/Musik/Ton A.wav", wav(3, 440))
	g.write("Public/Musik/Ton C.wav", wav(3, 523.25))
	g.write("Public/Wallpaper/Hafen.png", g.screenshot(2560, 1440))
}

func (g *gen) scans() {
	for i := 0; i < 15; i++ {
		d := epoch.AddDate(0, 0, -9*i)
		g.write(fmt.Sprintf("Scans/%s Scan %02d.pdf", d.Format("2006-01-02"), i+1), pdf(fmt.Sprintf("Scan %d", i+1), 1+i%3))
	}
}

// ── Files ───────────────────────────────────────────────────────────────────

// photo is a JPEG that looks like something — a sky, a horizon, a sun and
// some shapes, in a palette of its own — w×h as a person sees it, with an
// EXIF orientation tag. The scene is drawn upright and its pixels stored
// turned the other way, as a phone stores them: a viewer that honours the
// tag shows it upright, one that ignores it shows it sideways.
func (g *gen) photo(w, h, orientation int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	hue := g.r.Float64()
	sky := hsv(hue, 0.35, 0.95)
	ground := hsv(math.Mod(hue+0.35, 1), 0.55, 0.55)
	horizon := h/2 + g.r.Intn(h/4) - h/8
	sunX, sunY, sunR := g.r.Intn(w), g.r.Intn(horizon), h/10+g.r.Intn(h/10)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var c color.RGBA
			if y < horizon {
				t := float64(y) / float64(horizon)
				c = mix(sky, color.RGBA{255, 255, 255, 255}, t*0.6)
			} else {
				t := float64(y-horizon) / float64(h-horizon)
				c = mix(ground, color.RGBA{20, 20, 20, 255}, t*0.7)
			}
			dx, dy := x-sunX, y-sunY
			if dx*dx+dy*dy < sunR*sunR {
				c = color.RGBA{255, 230, 160, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	// A few soft blocks, so thumbnails differ.
	for i := 0; i < 6; i++ {
		bx, by := g.r.Intn(w), horizon+g.r.Intn(max(1, h-horizon))
		bw, bh := w/12+g.r.Intn(w/8), h/14+g.r.Intn(h/8)
		col := hsv(g.r.Float64(), 0.5, 0.7)
		for y := by; y < min(h, by+bh); y++ {
			for x := bx; x < min(w, bx+bw); x++ {
				img.SetRGBA(x, y, col)
			}
		}
	}
	// Sensor-like noise: without it the JPEGs compress to a fraction of what
	// a camera writes, and downloads and thumbnails would be measured on
	// files of the wrong size.
	for i := 0; i < len(img.Pix); i += 4 {
		n := g.r.Intn(25) - 12
		for c := 0; c < 3; c++ {
			img.Pix[i+c] = uint8(max(0, min(255, int(img.Pix[i+c])+n)))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, stored(img, orientation), &jpeg.Options{Quality: 88}); err != nil {
		log.Fatal(err)
	}
	return withOrientation(buf.Bytes(), orientation)
}

// stored turns an upright image into the pixels a camera stores with the
// given orientation tag: the inverse of what the tag tells a viewer to do.
func stored(up *image.RGBA, o int) *image.RGBA {
	b := up.Bounds()
	w, h := b.Dx(), b.Dy()
	if o == 1 {
		return up
	}
	sw, sh := w, h
	if o >= 5 {
		sw, sh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sx, sy int
			switch o {
			case 3: // the viewer turns it half round
				sx, sy = w-1-x, h-1-y
			case 6: // the viewer turns it a quarter clockwise: store it a quarter counter-clockwise
				sx, sy = y, w-1-x
			case 8: // the viewer turns it a quarter counter-clockwise: store it a quarter clockwise
				sx, sy = h-1-y, x
			default:
				sx, sy = x, y
			}
			out.SetRGBA(sx, sy, up.RGBAAt(x, y))
		}
	}
	return out
}

// withOrientation inserts an EXIF segment with one tag, Orientation, after
// the JPEG's start marker.
func withOrientation(b []byte, o int) []byte {
	tiff := []byte("II*\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00")
	tiff = append(tiff, byte(o), 0, 0, 0, 0, 0, 0, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := append([]byte{}, b[:2]...)
	out = append(out, 0xFF, 0xE1, byte((len(seg)+2)>>8), byte(len(seg)+2))
	out = append(out, seg...)
	return append(out, b[2:]...)
}

func (g *gen) screenshot(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := hsv(g.r.Float64(), 0.15, 0.97)
	bar := hsv(g.r.Float64(), 0.6, 0.45)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := bg
			switch {
			case y < h/14:
				c = bar
			case x < w/6:
				c = mix(bg, bar, 0.15)
			case (y/(h/24))%2 == 0 && x > w/5 && x < w-w/10 && y > h/8:
				c = mix(bg, color.RGBA{0, 0, 0, 255}, 0.06)
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// pdf is a valid PDF with a page per page, each with its title and number.
func pdf(title string, pages int) []byte {
	var b bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	b.WriteString("%PDF-1.4\n")
	kids := make([]string, pages)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	for i := 0; i < pages; i++ {
		text := fmt.Sprintf("BT /F1 28 Tf 72 720 Td (%s) Tj ET\nBT /F1 12 Tf 72 690 Td (Seite %d von %d - ein Beispieldokument des Mock-NAS) Tj ET",
			latin1(title), i+1, pages)
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(text), text))
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

// latin1 makes a PDF string of WinAnsi bytes, escaping what needs it.
func latin1(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x80:
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, "\\%03o", r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// wav is seconds of a sine tone, 16-bit mono at 22.05 kHz, playable.
func wav(seconds int, freq float64) []byte {
	const rate = 22050
	n := rate * seconds
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("RIFF")
	_ = binary.Write(&b, le, uint32(36+2*n))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(2 * rate), uint16(2), uint16(16)} {
		_ = binary.Write(&b, le, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, le, uint32(2*n))
	for i := 0; i < n; i++ {
		fade := math.Min(1, math.Min(float64(i), float64(n-i))/2000)
		_ = binary.Write(&b, le, int16(9000*fade*math.Sin(2*math.Pi*freq*float64(i)/rate)))
	}
	return b.Bytes()
}

func hsv(h, s, v float64) color.RGBA {
	i := math.Floor(h * 6)
	f := h*6 - i
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	var r, g, b float64
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return color.RGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), 255}
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

const notizen = `# Notizen

## Diese Woche

- [x] Fotos vom See sortieren
- [ ] Rechnungen für die Steuer scannen
- [ ] Sam wegen Urlaub fragen

## Ideen

Ein Regal für den Flur, aus Eiche. **Maße:** 120 × 30 cm.
`

const steuern = `# Steuern 2025

| Beleg | Betrag |
|---|---|
| Fahrrad | 849,00 € |
| Brille | 312,50 € |
| Handwerker Bad | 1.240,00 € |

Alles in *Belege/*, nach Quartal und Monat.
`

const apfelkuchen = `# Apfelkuchen

**Für eine Springform, 26 cm.**

- 250 g Mehl
- 125 g Butter
- 100 g Zucker
- 1 Ei
- 1 kg Äpfel, säuerlich

1. Mürbeteig kneten, 30 Minuten kühlen.
2. Äpfel schälen, in Spalten schneiden.
3. 50 Minuten bei 180 °C.
`
