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
