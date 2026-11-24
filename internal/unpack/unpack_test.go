package unpack

import (
	"errors"
	"strings"
	"testing"

	"github.com/denisneuling/ferry/internal/zipread"
)

func f(name string, size int64) zipread.Entry {
	return zipread.Entry{Name: name, Dir: strings.HasSuffix(name, "/"), Method: 8, Size: size}
}

var lim = Limits{Entries: 100, Depth: 4, Bytes: 1000}

func TestMake(t *testing.T) {
	p, err := Make([]zipread.Entry{
		f("Urlaub/", 0),
		f("Urlaub/Tag 1/IMG_1.jpg", 10),
		f("./Urlaub/Tag 2/IMG_2.jpg", 20),
		f("Notizen.txt", 5),
		{Name: "link", Link: true},
	}, lim)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Folders, "|") != "Urlaub|Urlaub/Tag 1|Urlaub/Tag 2" || len(p.Files) != 3 || p.Bytes != 35 || p.Links != 1 {
		t.Fatalf("%+v", p)
	}
}

// Archives from Windows that write backslashes as separators unpack; on
// Unix, with slashes, a backslash is part of a name and refused.
func TestWindowsSeparators(t *testing.T) {
	p, err := Make([]zipread.Entry{f(`dir\sub\a.txt`, 1), {Name: `notes\b.txt`, Method: 8, MadeBy: 3}}, lim)
	if err != nil {
		t.Fatal(err)
	}
	if p.Files[0].Path != "dir/sub/a.txt" || p.Files[1].Path != "notes/b.txt" || strings.Join(p.Folders, "|") != "dir|notes|dir/sub" {
		t.Fatalf("%+v", p)
	}
}

func TestRefused(t *testing.T) {
	for name, c := range map[string]struct {
		entries []zipread.Entry
		want    error
	}{
		"up":              {[]zipread.Entry{f("../etc/passwd", 1)}, ErrUnsafeName},
		"up inside":       {[]zipread.Entry{f("a/../../b", 1)}, ErrUnsafeName},
		"absolute":        {[]zipread.Entry{f("/etc/passwd", 1)}, ErrUnsafeName},
		"drive":           {[]zipread.Entry{f("C:/x", 1)}, ErrForbiddenName},
		"backslash up":    {[]zipread.Entry{f(`a\..\..\x`, 1)}, ErrUnsafeName},
		"unix backslash":  {[]zipread.Entry{{Name: `a/b\c`, Method: 8, MadeBy: 3}}, ErrForbiddenName},
		"star":            {[]zipread.Entry{f("a*.txt", 1)}, ErrForbiddenName},
		"control":         {[]zipread.Entry{f("a\x01", 1)}, ErrForbiddenName},
		"not text":        {[]zipread.Entry{f("a\xff", 1)}, ErrUnsafeName},
		"case":            {[]zipread.Entry{f("Readme.md", 1), f("README.md", 1)}, ErrCollision},
		"normalisation":   {[]zipread.Entry{f("Gr\u00fcn.txt", 1), f("Gru\u0308n.txt", 1)}, ErrCollision},
		"file and folder": {[]zipread.Entry{f("a", 1), f("a/b", 1)}, ErrCollision},
		"folder case":     {[]zipread.Entry{f("Docs/a", 1), f("docs/b", 1)}, ErrCollision},
		"encrypted":       {[]zipread.Entry{{Name: "x", Flags: 1, Method: 8}}, ErrUnreadable},
		"deep":            {[]zipread.Entry{f("1/2/3/4/5/x", 1)}, ErrTooDeep},
		"large":           {[]zipread.Entry{f("a", 600), f("b", 600)}, zipread.ErrTooLarge},
		"empty":           {[]zipread.Entry{f("./", 0), f(".", 1)}, ErrUnsafeName},
	} {
		if _, err := Make(c.entries, lim); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	many := make([]zipread.Entry, 101)
	for i := range many {
		many[i] = f(strings.Repeat("x", i+1), 1)
	}
	if _, err := Make(many, lim); !errors.Is(err, ErrTooMany) {
		t.Errorf("many: %v", err)
	}
}

func FuzzMake(fz *testing.F) {
	fz.Add("a/b/c.txt", "A/B/d.txt", "../x")
	fz.Add("x/", "x", "./y")
	fz.Fuzz(func(t *testing.T, a, b, c string) {
		p, err := Make([]zipread.Entry{f(a, 1), f(b, 2), f(c, 3)}, lim)
		if err != nil {
			return
		}
		for _, path := range append(append([]string{}, p.Folders...), func() []string {
			var s []string
			for _, x := range p.Files {
				s = append(s, x.Path)
			}
			return s
		}()...) {
			if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, `\:*?"<>|`) {
				t.Fatalf("unsafe path %q", path)
			}
			for _, part := range strings.Split(path, "/") {
				if part == "" || part == "." || part == ".." {
					t.Fatalf("unsafe part in %q", path)
				}
			}
		}
	})
}
