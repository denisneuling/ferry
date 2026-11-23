package smb

import (
	"strings"
	"unicode/utf8"
)

// Bounds on what a path or a share name may be before anything reaches the
// server. SMB2 itself allows a path of 32767 UTF-16 code units; nobody browses
// that deep, and a bound keeps every request small.
const (
	MaxPathBytes      = 4096
	MaxComponentBytes = 255
	MaxShareNameBytes = 80
)

// CleanPath turns a path from the API — slash-separated, relative to the
// share's root — into the backslash-separated form SMB expects, or refuses it.
// It refuses a component that begins with "@GMT-" as well.
//
// It refuses what could leave the share or confuse the server about where a
// name ends: `..`, NUL bytes, backslashes (a separator to SMB, never part of a
// name) and invalid UTF-8. Leading, trailing and repeated slashes and `.`
// components are harmless and dropped, so `/`, `.` and the empty path all mean the root,
// which is returned as the empty string. Every other character is passed on:
// which names are valid is the server's decision.
func CleanPath(p string) (string, error) {
	if len(p) > MaxPathBytes || !utf8.ValidString(p) {
		return "", &Error{Kind: KindInvalidPath}
	}
	if strings.ContainsAny(p, "\x00\\") {
		return "", &Error{Kind: KindInvalidPath}
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, c := range parts {
		switch {
		case c == "" || c == ".":
			continue
		case c == "..":
			return "", &Error{Kind: KindInvalidPath}
		case len(c) > MaxComponentBytes:
			return "", &Error{Kind: KindInvalidPath}
		case len(c) >= 5 && strings.EqualFold(c[:5], "@GMT-"):
			// A snapshot token: older servers read one in a path as a
			// time to open the path at. A version is asked for with its
			// own parameter (versions.go), never through the path.
			return "", &Error{Kind: KindInvalidPath}
		}
		out = append(out, c)
	}
	return strings.Join(out, `\`), nil
}

// validShareName refuses share names that are not a single plain name.
func validShareName(name string) bool {
	return name != "" && len(name) <= MaxShareNameBytes && utf8.ValidString(name) &&
		!strings.ContainsAny(name, "\x00\\/")
}
