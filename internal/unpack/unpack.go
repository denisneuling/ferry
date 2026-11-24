// Package unpack decides what unpacking a ZIP archive onto a share would
// write, before anything is written: every entry's place below the new
// folder, checked so that nothing lands outside it, no name is one an SMB
// server refuses or two names become one there, and the whole stays within
// bounds. An archive that fails any check is refused whole — nothing is
// renamed or left out silently, but symbolic links, which are not files,
// are passed over and counted.
package unpack

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/denisneuling/ferry/internal/zipread"
)

var (
	// ErrUnsafeName is an entry whose name would leave the folder, or is
	// empty, absolute, or not text.
	ErrUnsafeName = errors.New("unpack: an entry's name is not a safe path")
	// ErrForbiddenName is a name with a character an SMB server refuses:
	// \ : * ? " < > |, or a control character.
	ErrForbiddenName = errors.New("unpack: an entry's name has a character SMB refuses")
	// ErrCollision is two entries whose names are one on the share: equal
	// but for case or Unicode normalisation, or a file where a folder is.
	ErrCollision = errors.New("unpack: two entries would get the same name")
	// ErrUnreadable is an entry ferry cannot unpack: encrypted, or packed
	// otherwise than Store or Deflate.
	ErrUnreadable = errors.New("unpack: an entry cannot be unpacked")
	// ErrTooMany is more entries than the bound.
	ErrTooMany = errors.New("unpack: too many entries")
	// ErrTooDeep is an entry more folders deep than the bound.
	ErrTooDeep = errors.New("unpack: too deep")
)

// Limits bound one unpacking.
type Limits struct {
	Entries int   // files and folders written
	Depth   int   // folders below the new one
	Bytes   int64 // the files' sizes added up; 0: no bound of its own
}

// File is one file to write: its entry, and its path below the new folder.
type File struct {
	Entry zipread.Entry
	Path  string
}

// Plan is what unpacking writes: the folders, parents first, then the
// files, and their sizes added up.
type Plan struct {
	Folders []string
	Files   []File
	Bytes   int64
	// Links: symbolic links passed over.
	Links int
}

// Make checks an archive's entries and plans their unpacking.
func Make(entries []zipread.Entry, lim Limits) (Plan, error) {
	var p Plan
	folders := map[string]string{} // key → path
	files := map[string]bool{}     // keys of files
	addFolder := func(path string) {
		k := key(path)
		if _, ok := folders[k]; !ok {
			folders[k] = path
		}
	}
	for _, e := range entries {
		if e.Link {
			p.Links++
			continue
		}
		parts, err := clean(e.Name)
		if err != nil {
			return Plan{}, err
		}
		if len(parts) == 0 {
			if e.Dir {
				continue // "./": the folder itself
			}
			return Plan{}, ErrUnsafeName
		}
		depth := len(parts)
		if !e.Dir {
			depth-- // a file's own name is not a level
		}
		if depth > lim.Depth {
			return Plan{}, ErrTooDeep
		}
		for i := 1; i < len(parts); i++ {
			addFolder(strings.Join(parts[:i], "/"))
		}
		path := strings.Join(parts, "/")
		if e.Dir {
			addFolder(path)
			continue
		}
		if !e.Readable() {
			return Plan{}, ErrUnreadable
		}
		k := key(path)
		if files[k] {
			return Plan{}, ErrCollision
		}
		files[k] = true
		p.Files = append(p.Files, File{Entry: e, Path: path})
		p.Bytes += e.Size
		if lim.Bytes > 0 && p.Bytes > lim.Bytes {
			return Plan{}, zipread.ErrTooLarge
		}
	}
	// A folder and a file of the same name, and two folders that are one.
	seen := map[string]string{}
	for k, path := range folders {
		if files[k] {
			return Plan{}, ErrCollision
		}
		seen[k] = path
	}
	// Folder paths that differ only by case in a parent are one on the
	// share but two here: compare their written forms too.
	written := map[string]bool{}
	for _, path := range seen {
		written[path] = true
	}
	for _, f := range p.Files {
		if i := strings.LastIndexByte(f.Path, '/'); i >= 0 && !written[f.Path[:i]] {
			return Plan{}, ErrCollision
		}
	}
	if len(seen)+len(p.Files) > lim.Entries {
		return Plan{}, ErrTooMany
	}
	for _, path := range seen {
		p.Folders = append(p.Folders, path)
	}
	sort.Slice(p.Folders, func(i, j int) bool {
		a, b := strings.Count(p.Folders[i], "/"), strings.Count(p.Folders[j], "/")
		if a != b {
			return a < b
		}
		return p.Folders[i] < p.Folders[j]
	})
	return p, nil
}

// clean splits an entry's name into its parts, refusing what could leave
// the folder: an absolute name, a drive, a part that is . or .., and any
// character an SMB server refuses — a backslash among them, which some
// archivers write as a separator and others inside names.
func clean(name string) ([]string, error) {
	if !utf8.ValidString(name) || strings.HasPrefix(name, "/") {
		return nil, ErrUnsafeName
	}
	var parts []string
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return nil, ErrUnsafeName
		}
		for _, r := range part {
			if r < 0x20 || r == 0x7f || strings.ContainsRune(`\:*?"<>|`, r) {
				return nil, ErrForbiddenName
			}
		}
		if len(part) > 255 {
			return nil, ErrUnsafeName
		}
		parts = append(parts, norm.NFC.String(part))
	}
	return parts, nil
}

// key is how the share tells names apart: case aside, after NFC.
func key(path string) string {
	return strings.ToLower(norm.NFC.String(path))
}
