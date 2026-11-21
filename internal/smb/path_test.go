package smb

import (
	"strings"
	"testing"
)

func TestCleanPath(t *testing.T) {
	ok := map[string]string{
		"":                       "",
		"/":                      "",
		".":                      "",
		"//./":                   "",
		"Photos":                 "Photos",
		"/Photos/2026/":          `Photos\2026`,
		"Photos//./2026":         `Photos\2026`,
		"a b/c:d.txt":            `a b\c:d.txt`,
		"Fotos/Ü/ß.jpg":          `Fotos\Ü\ß.jpg`,
		"..hidden/file..":        `..hidden\file..`,
		strings.Repeat("a", 255): strings.Repeat("a", 255),
	}
	for in, want := range ok {
		got, err := CleanPath(in)
		if err != nil || got != want {
			t.Errorf("CleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"..",
		"../etc",
		"Photos/../../x",
		"/..",
		"a\x00b",
		`a\b`,
		`\\server\share`,
		"\xff\xfe",
		strings.Repeat("a", 256),
		strings.Repeat("a/", MaxPathBytes/2+1),
	}
	for _, in := range bad {
		if got, err := CleanPath(in); err == nil || KindOf(err) != KindInvalidPath {
			t.Errorf("CleanPath(%q) = %q, %v; want KindInvalidPath", in, got, err)
		}
	}
}

func TestValidShareName(t *testing.T) {
	for _, n := range []string{"alex", "Family", "Fotos & Videos", "x$"} {
		if !validShareName(n) {
			t.Errorf("%q refused", n)
		}
	}
	for _, n := range []string{"", "a/b", `a\b`, "a\x00", strings.Repeat("s", 81)} {
		if validShareName(n) {
			t.Errorf("%q accepted", n)
		}
	}
}
