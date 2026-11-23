package api

import "testing"

func TestEntryBase(t *testing.T) {
	for in, want := range map[string]string{
		"a/b/c.txt":          "c.txt",
		`dir\win.txt`:        "win.txt",
		"../../etc/passwd":   "passwd",
		"x/\x01bell\x7f.txt": "bell.txt",
		"/":                  "entry",
		"..":                 "entry",
		"Fotos/Grüße ☕.jpg":  "Grüße ☕.jpg",
	} {
		if got := entryBase(in); got != want {
			t.Errorf("entryBase(%q) = %q, want %q", in, got, want)
		}
	}
}
