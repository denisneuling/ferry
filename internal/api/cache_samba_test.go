package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/jpeg"
	"sync"
	"testing"
	"time"

	"github.com/metapatrol/ferry/internal/cache"
)

// cacheCounts counts what the cache did, by kind and outcome.
type cacheCounts struct {
	mu  sync.Mutex
	got map[string]int
}

func (c *cacheCounts) CacheRequest(kind, outcome string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got[kind+"/"+outcome]++
}
func (c *cacheCounts) CacheBytes(int64) {}
func (c *cacheCounts) CacheEvicted(int) {}

func (c *cacheCounts) n(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got[key]
}

func cachedHarness(t *testing.T) (*harness, *cacheCounts) {
	t.Helper()
	h := sambaHarness(t)
	obs := &cacheCounts{got: map[string]int{}}
	c, err := cache.Open(cache.Options{
		Dir: t.TempDir(), MaxBytes: 64 << 20, Secret: []byte("a test key, fictional, 32 bytes or more"),
		Scope: h.api.cfg.SMBServer, Observer: obs,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	h.api.UseCache(c)
	return h, obs
}

// until asks again until the cache has served a hit of kind, as entries are
// written in the background.
func until(t *testing.T, obs *cacheCounts, kind string, ask func()) {
	t.Helper()
	before := obs.n(kind + "/hit")
	for deadline := time.Now().Add(5 * time.Second); obs.n(kind+"/hit") == before; {
		if time.Now().After(deadline) {
			t.Fatalf("no %s hit", kind)
		}
		time.Sleep(20 * time.Millisecond)
		ask()
	}
}

// A thumbnail and a file's details come from the cache the second time, and
// not once the file has changed.
func TestCacheHits(t *testing.T) {
	h, obs := cachedHarness(t)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "cache-" + hex.EncodeToString(b)
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c)
	})
	put := func(w int) {
		var photo bytes.Buffer
		if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, w, 600)), nil); err != nil {
			t.Fatal(err)
		}
		if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/photo.jpg"), bytes.NewReader(photo.Bytes()), ferryReq, c); res.StatusCode != 201 {
			t.Fatalf("upload: %d", res.StatusCode)
		}
	}
	put(800)
	thumb := func() int {
		res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=160&path="+q(dir+"/photo.jpg"), nil, nil, c)
		if res.StatusCode != 200 {
			t.Fatalf("thumbnail: %d", res.StatusCode)
		}
		img, err := jpeg.Decode(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return img.Bounds().Dy()
	}
	details := func() {
		if res := h.do(t, "GET", "/api/v1/shares/alex/metadata?path="+q(dir+"/photo.jpg"), nil, nil, c); res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("details: %d", res.StatusCode)
		}
	}
	if thumb() != 120 {
		t.Fatal("the first thumbnail")
	}
	details()
	until(t, obs, "thumbnail", func() {
		if thumb() != 120 {
			t.Fatal("a cached thumbnail of another file")
		}
	})
	until(t, obs, "metadata", details)

	// Another picture under the same name: other size, other entry.
	misses := obs.n("thumbnail/miss")
	h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(dir+"/photo.jpg"), nil, ferryReq, c)
	put(400)
	if thumb() != 160 { // 400×600 fits the square by its height
		t.Fatal("the changed file's thumbnail is the old one")
	}
	if obs.n("thumbnail/miss") != misses+1 {
		t.Fatalf("the changed file was not a miss: %v", obs.got)
	}
}

// What the cache holds for a file reaches only those who may read it: alex
// sees sam's files in Family and may not open them, so a thumbnail or the
// details sam's requests left in the cache stay there.
func TestCacheNeedsReading(t *testing.T) {
	h, obs := cachedHarness(t)
	sam := h.login(t, "sam", "sam-test-password")
	alex := h.login(t, "alex", alexPassword)
	for _, c := range []struct{ kind, path string }{
		{"thumbnail", "/api/v1/shares/Family/thumbnail?path=sam-only.png"},
		{"metadata", "/api/v1/shares/Family/metadata?path=sam-only.txt"},
	} {
		ask := func() {
			if res := h.do(t, "GET", c.path, nil, nil, sam); res.StatusCode != 200 {
				t.Fatalf("sam's own %s: %d", c.kind, res.StatusCode)
			}
		}
		ask()
		until(t, obs, c.kind, ask) // in the cache now

		hits := obs.n(c.kind + "/hit")
		res := h.do(t, "GET", c.path, nil, nil, alex)
		if res.StatusCode != 403 || problemCode(t, res) != "access_denied" {
			t.Fatalf("alex, %s of a file alex may not read: %d", c.kind, res.StatusCode)
		}
		if obs.n(c.kind+"/hit") != hits {
			t.Fatalf("the cache was asked for alex: %v", obs.got)
		}
	}
	// alex does see the files: the listing has them.
	res := h.do(t, "GET", "/api/v1/shares/Family/entries", nil, nil, alex)
	if res.StatusCode != 200 {
		t.Fatalf("listing: %d", res.StatusCode)
	}
	listing := new(bytes.Buffer)
	_, _ = listing.ReadFrom(res.Body)
	if !bytes.Contains(listing.Bytes(), []byte("sam-only.png")) {
		t.Fatal("alex does not see sam's file, so the test proves nothing")
	}
}
