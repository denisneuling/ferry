package cache

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var secret = []byte("a test key, fictional, 32 bytes or more")

type counts struct {
	mu      sync.Mutex
	got     map[string]int
	bytes   int64
	evicted int
}

func (c *counts) CacheRequest(kind, outcome string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.got == nil {
		c.got = map[string]int{}
	}
	c.got[kind+"/"+outcome]++
}
func (c *counts) CacheBytes(n int64) { c.mu.Lock(); c.bytes = n; c.mu.Unlock() }
func (c *counts) CacheEvicted(n int) { c.mu.Lock(); c.evicted += n; c.mu.Unlock() }

func (c *counts) n(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got[key]
}

func open(t *testing.T, dir string, max int64, obs Observer) *Cache {
	t.Helper()
	c, err := Open(Options{Dir: dir, MaxBytes: max, Secret: secret, Scope: "nas.example.lan:445", Observer: obs})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var when = time.Date(2025, 7, 14, 10, 30, 0, 0, time.UTC)

func key(path string) Key {
	return Key{Kind: "thumbnail", Variant: "320", Share: "alex", Path: path, Size: 1234, ModTime: when}
}

// A JPEG's first bytes and a path's parts, to look for on the disk.
var picture = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte("pixels "), 200)...)

func TestPutGet(t *testing.T) {
	dir := t.TempDir()
	obs := &counts{}
	c := open(t, dir, 1<<20, obs)
	k := key("Photos/2025 Sommer am See/IMG_2117.jpg")
	if _, ok := c.Get(k); ok {
		t.Fatal("a hit in an empty cache")
	}
	c.Put(k, picture)
	c.Close() // writes what is queued

	// Another process, as after a restart, finds it.
	c = open(t, dir, 1<<20, obs)
	defer c.Close()
	got, ok := c.Get(k)
	if !ok || !bytes.Equal(got, picture) {
		t.Fatalf("after a restart: %v", ok)
	}
	if obs.n("thumbnail/hit") != 1 || obs.n("thumbnail/miss") != 1 {
		t.Fatalf("counted %v", obs.got)
	}

	// Anything else about the file, or another cache, is another entry.
	for name, other := range map[string]Key{
		"size":    {Kind: k.Kind, Variant: k.Variant, Share: k.Share, Path: k.Path, Size: k.Size + 1, ModTime: k.ModTime},
		"time":    {Kind: k.Kind, Variant: k.Variant, Share: k.Share, Path: k.Path, Size: k.Size, ModTime: k.ModTime.Add(time.Second)},
		"variant": {Kind: k.Kind, Variant: "640", Share: k.Share, Path: k.Path, Size: k.Size, ModTime: k.ModTime},
		"kind":    {Kind: "metadata", Variant: k.Variant, Share: k.Share, Path: k.Path, Size: k.Size, ModTime: k.ModTime},
		"share":   {Kind: k.Kind, Variant: k.Variant, Share: "sam", Path: k.Path, Size: k.Size, ModTime: k.ModTime},
		"path":    {Kind: k.Kind, Variant: k.Variant, Share: k.Share, Path: k.Path + "x", Size: k.Size, ModTime: k.ModTime},
		// Fields that run into each other must not make the same identity.
		"boundary": {Kind: k.Kind, Variant: k.Variant, Share: "alexPhotos", Path: strings.TrimPrefix(k.Path, "Photos"), Size: k.Size, ModTime: k.ModTime},
	} {
		if _, ok := c.Get(other); ok {
			t.Errorf("another %s hits", name)
		}
	}
	for name, o := range map[string]Options{
		"server": {Dir: dir, MaxBytes: 1 << 20, Secret: secret, Scope: "other.example.lan:445"},
		"key":    {Dir: dir, MaxBytes: 1 << 20, Secret: append([]byte("another "), secret...), Scope: "nas.example.lan:445"},
	} {
		c2, err := Open(o)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := c2.Get(k); ok {
			t.Errorf("another %s hits", name)
		}
		c2.Close()
	}
}

// On the disk there are hex names and ciphertext: no part of a path, no
// share, nothing of the picture.
func TestNothingReadableOnDisk(t *testing.T) {
	dir := t.TempDir()
	c := open(t, dir, 1<<20, nil)
	for i, p := range []string{"Photos/2025 Sommer am See/IMG_2117.jpg", "Documents/Steuer 2025.pdf"} {
		k := key(p)
		k.Size += int64(i)
		c.Put(k, picture)
	}
	c.Close()
	files := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		name := d.Name()
		if len(name) != 64 || strings.Trim(name, "0123456789abcdef") != "" {
			t.Errorf("an entry named %q", name)
		}
		b, _ := os.ReadFile(p)
		for _, s := range [][]byte{[]byte("Photos"), []byte("Sommer"), []byte("IMG_2117"), []byte("Steuer"), []byte("alex"), []byte("pixels"), {0xFF, 0xD8, 0xFF}} {
			if bytes.Contains(b, s) || strings.Contains(p, string(s)) {
				t.Errorf("%q is readable in an entry", s)
			}
		}
		return nil
	})
	if files != 2 {
		t.Fatalf("%d entries", files)
	}
}

// A damaged entry is a miss, and it is removed, so that the next Put
// replaces it.
func TestDamagedEntry(t *testing.T) {
	dir := t.TempDir()
	c := open(t, dir, 1<<20, nil)
	k := key("a.jpg")
	c.Put(k, picture)
	c.Close()
	p := c.path(c.name(k))
	b, _ := os.ReadFile(p)
	b[len(b)/2] ^= 1
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	obs := &counts{}
	c = open(t, dir, 1<<20, obs)
	defer c.Close()
	if _, ok := c.Get(k); ok {
		t.Fatal("a damaged entry hits")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("the damaged entry is still there: %v", err)
	}
	if obs.n("thumbnail/error") != 1 {
		t.Fatalf("counted %v", obs.got)
	}
}

// Over its size, the cache removes the entries used least recently — by
// the time a hit sets, not the access time a file system may not keep.
func TestSweep(t *testing.T) {
	dir := t.TempDir()
	entry := len(picture) + 1 + 12 + 16 // version, nonce, tag
	limit := int64(entry * 10)
	obs := &counts{}
	other := func(i int) Key {
		k := key("other.jpg")
		k.Size = int64(i)
		return k
	}
	// Ten entries, as many as fit: keep the oldest of them.
	c := open(t, dir, limit, obs)
	keep := key("keep.jpg")
	c.Put(keep, picture)
	for i := range 9 {
		c.Put(other(i), picture)
	}
	c.Close()
	base := time.Now().Add(-time.Hour)
	_ = os.Chtimes(c.path(c.name(keep)), base.Add(-time.Hour), base.Add(-time.Hour))
	for i := range 9 {
		at := base.Add(time.Duration(i) * time.Second)
		_ = os.Chtimes(c.path(c.name(other(i))), at, at)
	}

	// keep is used; then two more push the cache over its size.
	c = open(t, dir, limit, obs)
	if _, ok := c.Get(keep); !ok {
		t.Fatal("no hit")
	}
	c.Put(other(9), picture)
	c.Put(other(10), picture)
	c.Close()

	c = open(t, dir, limit, obs)
	defer c.Close()
	if _, ok := c.Get(keep); !ok {
		t.Fatal("the entry used last was swept")
	}
	if total := c.total.Load(); total > limit {
		t.Fatalf("%d bytes after sweeping, over %d", total, limit)
	}
	if obs.evicted == 0 {
		t.Fatal("no eviction counted")
	}
	if _, ok := c.Get(other(0)); ok {
		t.Fatal("the entry used longest ago is still there")
	}
	if _, ok := c.Get(other(10)); !ok {
		t.Fatal("the newest entry was swept")
	}
}

// A volume that cannot be written to makes misses and a warning, never a
// failure or a panic.
func TestReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anyway")
	}
	dir := t.TempDir()
	obs := &counts{}
	c := open(t, dir, 1<<20, obs)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	k := key("a.jpg")
	c.Put(k, picture)
	c.Close()
	if obs.n("thumbnail/error") != 1 {
		t.Fatalf("counted %v", obs.got)
	}
	if _, ok := c.Get(k); ok {
		t.Fatal("a hit on a volume never written")
	}
}

func TestBounds(t *testing.T) {
	if _, err := Open(Options{Dir: t.TempDir(), MaxBytes: 1 << 20, Secret: []byte("short")}); err == nil {
		t.Fatal("a short key was taken")
	}
	if _, err := Open(Options{Dir: t.TempDir(), Secret: secret}); err == nil {
		t.Fatal("no size was taken")
	}
	dir := t.TempDir()
	c := open(t, dir, 64<<20, nil)
	c.Put(key("huge"), make([]byte, MaxEntry+1))
	c.Put(key("empty"), nil)
	c.Close()
	if total := c.total.Load(); total != 0 {
		t.Fatalf("%d bytes kept of entries out of bounds", total)
	}
	// An interrupted write's leftover is removed on the next start.
	left := filepath.Join(dir, "tmp", "w123")
	_ = os.WriteFile(left, []byte("half"), 0o600)
	c = open(t, dir, 64<<20, nil)
	c.Close()
	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Fatal("a leftover write is still there")
	}
}
