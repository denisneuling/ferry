package zipread

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

type file struct {
	name, body string
	store      bool
}

func makeZip(t testing.TB, files ...file) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		if f.store {
			h.Method = zip.Store
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(f.body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestListAndRead(t *testing.T) {
	big := strings.Repeat("ferry ", 10000)
	z := makeZip(t,
		file{name: "docProps/thumbnail.jpeg", body: "jpeg", store: true},
		file{name: "word/document.xml", body: big},
		file{name: "Fotos/"},
		file{name: "Größe ☕.txt", body: "umlauts"},
	)
	es, err := List(bytes.NewReader(z), int64(len(z)), 100)
	if err != nil || len(es) != 4 {
		t.Fatalf("list: %+v, %v", es, err)
	}
	if !es[2].Dir || es[3].Name != "Größe ☕.txt" {
		t.Fatalf("entries %+v", es)
	}
	e, _ := Find(es, "word/document.xml")
	got, err := ReadAll(bytes.NewReader(z), int64(len(z)), e, 1<<20)
	if err != nil || string(got) != big {
		t.Fatalf("read: %d bytes, %v", len(got), err)
	}
	if _, err := ReadAll(bytes.NewReader(z), int64(len(z)), e, 100); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("bounded: %v", err)
	}
	if _, err := List(bytes.NewReader(z), int64(len(z)), 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too many entries: %v", err)
	}
}

// A deflate stream that unpacks into more than its header declares is cut
// off with an error, never read on: what a ZIP bomb with a lying header is.
func TestMoreThanDeclared(t *testing.T) {
	z := makeZip(t, file{name: "a.txt", body: strings.Repeat("x", 5000)})
	es, _ := List(bytes.NewReader(z), int64(len(z)), 10)
	e := es[0]
	e.Size = 100 // the directory lies
	rd, err := Open(bytes.NewReader(z), int64(len(z)), e, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, rd)
	if !errors.Is(err, ErrCorrupt) || n > 100 {
		t.Fatalf("read %d, %v", n, err)
	}
	e.Size = 9000 // or claims more than there is
	rd, _ = Open(bytes.NewReader(z), int64(len(z)), e, 1<<20)
	if _, err := io.Copy(io.Discard, rd); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("short: %v", err)
	}
}

func TestRefuses(t *testing.T) {
	z := makeZip(t, file{name: "secret.txt", body: "x"})
	es, _ := List(bytes.NewReader(z), int64(len(z)), 10)
	e := es[0]
	e.Flags |= 1
	if _, err := Open(bytes.NewReader(z), int64(len(z)), e, 100); !errors.Is(err, ErrUnsupported) || e.Readable() {
		t.Fatalf("encrypted: %v", err)
	}
	e.Flags, e.Method = 0, 12 // bzip2
	if _, err := Open(bytes.NewReader(z), int64(len(z)), e, 100); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("bzip2: %v", err)
	}
	for _, b := range [][]byte{nil, []byte("PK\x03\x04 not a zip"), []byte(strings.Repeat("\x00", 100))} {
		if _, err := List(bytes.NewReader(b), int64(len(b)), 10); !errors.Is(err, ErrNotZip) {
			t.Errorf("%q: %v", b, err)
		}
	}
	// A directory that claims to lie beyond the file.
	end := bytes.LastIndex(z, []byte("PK\x05\x06"))
	bad := append([]byte{}, z...)
	binary.LittleEndian.PutUint32(bad[end+16:], uint32(len(z)+10))
	if _, err := List(bytes.NewReader(bad), int64(len(bad)), 10); !errors.Is(err, ErrNotZip) {
		t.Fatalf("directory beyond the file: %v", err)
	}
}

type countingReaderAt struct {
	r    *bytes.Reader
	read int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.read += int64(n)
	return n, err
}

// FuzzList feeds List and Open whatever: no panic, and no entry yields
// more than it declares or more than the bound asked for.
func FuzzList(f *testing.F) {
	f.Add(makeZip(f, file{name: "a.txt", body: "hello"}, file{name: "b/", body: ""}, file{name: "c.bin", body: strings.Repeat("ab", 300), store: true}))
	f.Add([]byte("PK\x05\x06" + strings.Repeat("\x00", 18)))
	f.Fuzz(func(t *testing.T, b []byte) {
		r := &countingReaderAt{r: bytes.NewReader(b)}
		es, err := List(r, int64(len(b)), 64)
		if err != nil {
			return
		}
		if r.read > tail+20+56+MaxDirectory {
			t.Fatalf("listing read %d bytes", r.read)
		}
		for _, e := range es {
			rd, err := Open(r, int64(len(b)), e, 1<<16)
			if err != nil {
				continue
			}
			n, _ := io.Copy(io.Discard, rd)
			if n > e.Size || n > 1<<16 {
				t.Fatalf("%q yielded %d of %d", e.Name, n, e.Size)
			}
		}
	})
}
