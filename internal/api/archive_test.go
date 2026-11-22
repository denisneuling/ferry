package api

import "testing"

func TestZipName(t *testing.T) {
	for _, c := range []struct{ base, path, want string }{
		{"", "notes.txt", "notes.txt"},
		{"", "Fotos/Strand.jpg", "Fotos/Strand.jpg"},
		{"Urlaub/2025", "Urlaub/2025/Fotos/Strand.jpg", "Fotos/Strand.jpg"},
		{"Urlaub", "Urlaub/a\\..\\..\\evil.txt", "a_.._.._evil.txt"},
		{"", "Übersicht ä 🏔️.md", "Übersicht ä 🏔️.md"},
	} {
		if got := zipName(c.base, c.path); got != c.want {
			t.Errorf("zipName(%q, %q) = %q, want %q", c.base, c.path, got, c.want)
		}
	}
}

func TestIsHEIC(t *testing.T) {
	for h, want := range map[string]bool{
		"\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic": true,
		"\x00\x00\x00\x18ftypmif1\x00\x00\x00\x00mif1heic": true,
		"\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1avif": false,
		"\x00\x00\x00\x14ftypM4A \x00\x00\x00\x00M4A ":     false,
		"\x00\x00\x00\x10ftyp":                             false,
		"%PDF-1.7":                                         false,
	} {
		if isHEIC([]byte(h)) != want {
			t.Errorf("isHEIC(%q) is not %v", h, want)
		}
	}
}
