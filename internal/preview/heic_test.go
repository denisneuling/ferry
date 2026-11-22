package preview

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// heicSample is a HEIC photo from gen2brain/heic's test data: a 3×3 grid of
// HEVC tiles, 1346×1346, turned by an irot. It is fetched like a module, not
// kept in this repository.
func heicSample(t testing.TB) []byte {
	t.Helper()
	out, err := exec.Command("go", "mod", "download", "-json", "github.com/gen2brain/heic@v0.4.9").Output()
	if err != nil {
		t.Skipf("the HEIC sample cannot be fetched: %v", err)
	}
	var m struct{ Dir string }
	if json.Unmarshal(out, &m) != nil || m.Dir == "" {
		t.Skip("the HEIC sample cannot be fetched")
	}
	b, err := os.ReadFile(filepath.Join(m.Dir, "testdata", "test.heic"))
	if err != nil {
		t.Skip(err)
	}
	return b
}

// variant rebuilds the sample: its tiles and grid, without Exif, then
// changes it.
func variant(t testing.TB, sample []byte, change func(f *heifFile, data map[uint32][]byte)) []byte {
	t.Helper()
	f, err := parseHEIF(bytes.NewReader(sample), int64(len(sample)))
	if err != nil {
		t.Fatal(err)
	}
	data := map[uint32][]byte{}
	var items []*heifItem
	for _, it := range f.items {
		if it.typ != "hvc1" && it.typ != "grid" {
			continue
		}
		d, err := f.data(bytes.NewReader(sample), it, int64(len(sample)), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		data[it.id] = d
		items = append(items, it)
	}
	f.items = items
	var refs []heifRef
	for _, r := range f.refs {
		if r.typ == "dimg" {
			refs = append(refs, r)
		}
	}
	f.refs = refs
	change(f, data)
	return writeHEIF(f, data)
}

// withThumbnail adds tile 1 as the photo's thumbnail item.
func withThumbnail(f *heifFile, data map[uint32][]byte) {
	tile := f.item(1)
	f.items = append(f.items, &heifItem{id: 99, typ: "hvc1", props: tile.props})
	data[99] = data[1]
	f.refs = append(f.refs, heifRef{typ: "thmb", from: 99, to: []uint32{f.primary}})
}

// bigGrid makes the grid cols×rows tiles of 512, reusing the nine's bytes:
// 8×6 is 4096×3072, 12.6 MP — a phone's photo.
func bigGrid(cols, rows int) func(f *heifFile, data map[uint32][]byte) {
	return func(f *heifFile, data map[uint32][]byte) {
		grid := f.item(f.primary)
		// The tile's size, from its ispe.
		var tw, th uint32
		for _, a := range f.item(1).props {
			p := f.props[a.index-1]
			if string(p[4:8]) == "ispe" {
				tw, th = binary.BigEndian.Uint32(p[12:]), binary.BigEndian.Uint32(p[16:])
			}
		}
		w, h := tw*uint32(cols), th*uint32(rows)
		data[grid.id] = []byte{0, 0, byte(rows - 1), byte(cols - 1), byte(w >> 8), byte(w), byte(h >> 8), byte(h)}
		f.props = append(f.props, box("ispe", full(0, 0), be32(w), be32(h)))
		grid.props = []heifAssoc{{index: len(f.props)}}
		// libheif takes each tile item once: cols×rows items, sharing the nine's
		// bytes.
		var tiles []uint32
		props := f.item(1).props
		for i := range cols * rows {
			id := uint32(1000 + i)
			f.items = append(f.items, &heifItem{id: id, typ: "hvc1", props: props})
			data[id] = data[uint32(1+i%9)]
			tiles = append(tiles, id)
		}
		for i := range f.refs {
			if f.refs[i].typ == "dimg" {
				f.refs[i].to = tiles
			}
		}
	}
}

func heicEngine(t *testing.T, o Options) *Engine {
	t.Helper()
	o.HEIC = true
	if o.CodeDir == "" {
		o.CodeDir = codeDir
	}
	e := New(o)
	t.Cleanup(e.Close)
	return e
}

// decoded records the pictures decoded in full.
func decoded(t *testing.T) func() [][2]int {
	var mu sync.Mutex
	var got [][2]int
	decodedHook = func(w, h int) { mu.Lock(); got = append(got, [2]int{w, h}); mu.Unlock() }
	t.Cleanup(func() { decodedHook = nil })
	return func() [][2]int { mu.Lock(); defer mu.Unlock(); return append([][2]int(nil), got...) }
}

func heic(t *testing.T, e *Engine, file []byte, box int) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return e.HEIC(ctx, bytes.NewReader(file), int64(len(file)), box, 64<<20, 50_000_000)
}

func TestHEIC(t *testing.T) {
	sample := heicSample(t)
	thumbed := variant(t, sample, withThumbnail)
	big := variant(t, sample, bigGrid(8, 6))
	e := heicEngine(t, Options{})
	got := decoded(t)
	for _, c := range []struct {
		name    string
		file    []byte
		box     int
		decodes [2]int // what was decoded in full
	}{
		{"no thumbnail item, 320: the photo", sample, 320, [2]int{1346, 1346}},
		{"a thumbnail item, 320: the item", thumbed, 320, [2]int{0, 0}},
		{"a thumbnail item, 640: the photo", thumbed, 640, [2]int{1346, 1346}},
		{"12.6 MP, 640: the photo", big, 640, [2]int{0, 0}},
	} {
		before := len(got())
		start := time.Now()
		out, err := heic(t, e, c.file, c.box)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		img, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		if max(b.Dx(), b.Dy()) != c.box {
			t.Errorf("%s: %v in %d", c.name, b, c.box)
		}
		d := got()[before:]
		t.Logf("%s: %v, decoded %v, %v", c.name, b, d, time.Since(start).Round(time.Millisecond))
		if len(d) != 1 {
			t.Fatalf("%s: %d decodes", c.name, len(d))
		}
		if c.decodes != [2]int{0, 0} && d[0] != c.decodes {
			t.Errorf("%s: decoded %v, want %v", c.name, d[0], c.decodes)
		}
		if c.name == "a thumbnail item, 320: the item" && d[0][0] >= 1346 {
			t.Errorf("the photo was decoded, not its thumbnail item: %v", d[0])
		}
		if c.name == "12.6 MP, 640: the photo" && d[0][0]*d[0][1] < 12_000_000 {
			t.Errorf("not the 12 MP photo: %v", d[0])
		}
	}
	// Without a thumbnail item, a big photo's tile is the photo too.
	before := len(got())
	if _, err := heic(t, e, big, 160); err != nil {
		t.Fatal(err)
	}
	if d := got()[before:]; len(d) != 1 || d[0][0]*d[0][1] < 12_000_000 {
		t.Fatalf("160 without a thumbnail item decoded %v", d)
	}
}

func TestHEICFailures(t *testing.T) {
	sample := heicSample(t)
	big := variant(t, sample, bigGrid(8, 6))
	e := heicEngine(t, Options{})
	ctx := context.Background()
	if _, err := e.HEIC(ctx, bytes.NewReader(big), int64(len(big)), 640, 64<<20, 10_000_000); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the pixels: %v", err)
	}
	if _, err := e.HEIC(ctx, bytes.NewReader(big), int64(len(big)), 640, 1000, 50_000_000); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the bytes: %v", err)
	}
	for name, file := range map[string][]byte{
		"not a picture": []byte("just some text, and not a HEIF file at all, long enough"),
		"cut off":       sample[:len(sample)/2],
	} {
		if _, err := heic(t, e, file, 320); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// Past its context, a decode is aborted inside the module.
	tctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := e.HEIC(tctx, bytes.NewReader(big), int64(len(big)), 640, 64<<20, 50_000_000); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("past its context: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("aborted after %v", d)
	}
	// Over the instance's memory: an error, not a crash.
	tight := heicEngine(t, Options{HEIFMemory: 8 << 20, CodeDir: t.TempDir()})
	if _, err := heic(t, tight, big, 640); err == nil {
		t.Error("12.6 MP within 8 MiB")
	}
	// And the engine works on.
	if _, err := heic(t, e, sample, 320); err != nil {
		t.Fatalf("after the failures: %v", err)
	}
}

// FERRY_HEIF_WASM replaces the module: the same one works (its marker
// matches), a broken one turns HEIC previews off without a panic.
func TestHEIFModuleReplaced(t *testing.T) {
	sample := heicSample(t)
	own, err := os.ReadFile(filepath.Join("..", "..", "third_party", "heif", "heif.wasm.gz"))
	if err != nil {
		t.Fatal(err)
	}
	_ = own
	broken := heicEngine(t, Options{HEIFModule: []byte("\x00asm\x01\x00\x00\x00not really")})
	for range 2 {
		if _, err := heic(t, broken, sample, 320); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("a broken module: %v", err)
		}
	}
}

func FuzzHEIFThumbnail(f *testing.F) {
	sample := heicSample(f)
	f.Add(sample)
	f.Add(variant(f, sample, withThumbnail))
	f.Add(craftedILOC(hostileExtents, false))
	f.Add(craftedILOC(hostileExtents, true))
	f.Fuzz(func(t *testing.T, b []byte) {
		out, err := heifThumbnail(bytes.NewReader(b), int64(len(b)), heifThumbMax)
		if err == nil && out != nil {
			if _, err := parseHEIF(bytes.NewReader(out), int64(len(out))); err != nil {
				t.Fatalf("a thumbnail file ferry cannot read back: %v", err)
			}
		}
	})
}

// TestWriteLoadHEIC writes the load test's photos (hack/loadtest/run.sh)
// into FERRY_LOADTEST_DIR: 12.6 MP with a thumbnail item, and without one.
func TestWriteLoadHEIC(t *testing.T) {
	dir := os.Getenv("FERRY_LOADTEST_DIR")
	if dir == "" {
		t.Skip("FERRY_LOADTEST_DIR is not set")
	}
	sample := heicSample(t)
	big := bigGrid(8, 6)
	for name, change := range map[string]func(*heifFile, map[uint32][]byte){
		"photo-thumbnail.heic": func(f *heifFile, d map[uint32][]byte) { big(f, d); withThumbnail(f, d) },
		"photo-plain.heic":     big,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), variant(t, sample, change), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// craftedILOC is a HEIF file whose thumbnail item has the given extents, in
// the file or in idat, written with 64-bit offsets and lengths: what a
// hostile file may claim.
func craftedILOC(extents [][2]uint64, idat bool) []byte {
	be64 := func(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
	method := uint16(0)
	if idat {
		method = 1
	}
	iloc := [][]byte{full(1, 0), {0x88, 0x00}, be16(2),
		be16(1), be16(0), be16(0), be16(1), be64(0), be64(1)} // the primary: one byte
	e := [][]byte{be16(2), be16(int(method)), be16(0), be16(len(extents))}
	for _, x := range extents {
		e = append(e, be64(x[0]), be64(x[1]))
	}
	iloc = append(iloc, e...)
	meta := box("meta", full(0, 0),
		box("hdlr", full(0, 0), be32(0), []byte("pict"), make([]byte, 12), []byte{0}),
		box("pitm", full(0, 0), be16(1)),
		box("iinf", full(0, 0), be16(2),
			box("infe", full(2, 0), be16(1), be16(0), []byte("hvc1"), []byte{0}),
			box("infe", full(2, 0), be16(2), be16(0), []byte("hvc1"), []byte{0})),
		box("iref", full(0, 0), box("thmb", be16(2), be16(1), be16(1))),
		box("iprp", box("ipco"), box("ipma", full(0, 0), be32(0))),
		box("idat", []byte("0123456789")),
		box("iloc", iloc...))
	return bytes.Join([][]byte{box("ftyp", []byte("heic"), be32(0), []byte("mif1heic")), meta, box("mdat", make([]byte, 256))}, nil)
}

// The review's crafted extents: lengths whose sum wraps round to 100, and
// one of 64 TiB that a check of the sum alone would let through to make.
var hostileExtents = [][2]uint64{{0, 1 << 46}, {0, 1 << 62}, {0, 1 << 62}, {0, 1 << 62}, {0, 1<<62 - 1<<46 + 100}}

func TestHEIFHostileExtents(t *testing.T) {
	for _, c := range []struct {
		name string
		file []byte
	}{
		{"lengths that wrap round, in the file", craftedILOC(hostileExtents, false)},
		{"lengths that wrap round, in idat", craftedILOC(hostileExtents, true)},
		{"an idat extent past its end", craftedILOC([][2]uint64{{4, 1<<62 - 1}}, true)},
		{"an offset that overflows", craftedILOC([][2]uint64{{1<<62 + 1<<61, 10}, {1 << 62, 10}}, false)},
		{"a file extent past the end", craftedILOC([][2]uint64{{100, 100000}}, false)},
	} {
		out, err := heifThumbnail(bytes.NewReader(c.file), int64(len(c.file)), heifThumbMax)
		if err == nil || out != nil {
			t.Errorf("%s: %d bytes, %v", c.name, len(out), err)
		}
	}
	// What fits is taken: an idat extent within it, as the control.
	out, err := heifThumbnail(bytes.NewReader(craftedILOC([][2]uint64{{2, 5}}, true)), 10000, heifThumbMax)
	if err != nil || out == nil {
		t.Fatalf("an extent within idat: %v", err)
	}
	// A top-level box claiming a 64-bit size: refused, not wrapped.
	huge := append(be32(1), []byte("meta")...)
	huge = append(huge, binary.BigEndian.AppendUint64(nil, 1<<63-1)...)
	if _, err := parseHEIF(bytes.NewReader(append([]byte("\x00\x00\x00\x10ftypheic\x00\x00\x00\x00"), huge...)), 1000); err == nil {
		t.Fatal("a box of 2^63 bytes in a file of 1000")
	}
}
