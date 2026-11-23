package smb

import (
	"os"
	"strings"
	"testing"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
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
		// Snapshot tokens: a version has its own parameter, never the path.
		"@GMT-2026.11.23-13.00.09/letter.txt",
		"Docs/@gmt-2026.11.23-13.00.09",
		"Docs/@GMT-",
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

func TestParseToken(t *testing.T) {
	at, err := ParseToken("@GMT-2026.11.23-15.00.09")
	if err != nil || !at.Equal(time.Date(2026, 11, 23, 15, 0, 9, 0, time.UTC)) || Token(at) != "@GMT-2026.11.23-15.00.09" {
		t.Fatalf("got %v, %v", at, err)
	}
	for _, bad := range []string{
		"", "@GMT-2026.11.23-15.00.9", "@gmt-2026.11.23-15.00.09", "@GMT-2026.13.23-15.00.09",
		"@GMT-2026.11.23-15.00.09x", "@GMT-2026.11.23 15.00.09", "2026-11-23T15:00:09Z", "@GMT-2026.02.30-15.00.09",
	} {
		if _, err := ParseToken(bad); KindOf(err) != KindInvalidPath {
			t.Errorf("ParseToken(%q): %v", bad, err)
		}
	}
}

// Only a request the server does not know means "no snapshots"; a refusal,
// a lost connection or anything else stays an error.
func TestNoSnapshots(t *testing.T) {
	if !noSnapshots(&os.PathError{Op: "snapshots", Err: &smb2.ResponseError{Code: statusInvalidDeviceRequest}}) {
		t.Fatal("INVALID_DEVICE_REQUEST is not read as no snapshots")
	}
	for _, err := range []error{
		nil,
		&smb2.ResponseError{Code: statusAccessDenied},
		&smb2.ResponseError{Code: 0xC00000BB}, // NOT_SUPPORTED
		&smb2.ResponseError{Code: statusObjectNameNotFound},
		&smb2.TransportError{},
		os.ErrNotExist,
	} {
		if noSnapshots(err) {
			t.Errorf("%v is read as no snapshots", err)
		}
	}
}
