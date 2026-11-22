package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/jpeg"
	"testing"

	v1 "github.com/metapatrol/ferry/api/v1"
)

const immutable = "private, max-age=31536000, immutable"

// TestVersionedURLs holds that a thumbnail or details asked for with the
// version the listing gave are kept for a year, and only while the file is
// at that version: the listing's times and an open's agree on Samba, a
// changed file revalidates under its old URL, and no version, or a made-up
// one, is answered as before.
func TestVersionedURLs(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "version-" + hex.EncodeToString(b)
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c)
	})
	put := func(w, hgt int) {
		var photo bytes.Buffer
		if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, w, hgt)), nil); err != nil {
			t.Fatal(err)
		}
		if res := h.do(t, "PUT", "/api/v1/shares/alex/content?overwrite=true&path="+q(dir+"/photo.jpg"), bytes.NewReader(photo.Bytes()), ferryReq, c); res.StatusCode/100 != 2 {
			t.Fatalf("upload: %d", res.StatusCode)
		}
	}
	listed := func() string {
		list := decode[v1.EntryList](t, h.do(t, "GET", "/api/v1/shares/alex/entries?path="+q(dir), nil, nil, c))
		if len(list.Entries) != 1 || list.Entries[0].Version == "" {
			t.Fatalf("listing: %+v", list.Entries)
		}
		return list.Entries[0].Version
	}
	cache := func(what, v string) string {
		url := "/api/v1/shares/alex/" + what + "?path=" + q(dir+"/photo.jpg")
		if v != "" {
			url += "&v=" + q(v)
		}
		res := h.do(t, "GET", url, nil, nil, c)
		if res.StatusCode != 200 {
			t.Fatalf("%s v=%q: %d", what, v, res.StatusCode)
		}
		return res.Header.Get("Cache-Control")
	}

	put(640, 480)
	v := listed()
	for _, what := range []string{"thumbnail", "metadata"} {
		if got := cache(what, v); got != immutable {
			t.Errorf("%s at the listed version: %q", what, got)
		}
		for _, other := range []string{"", "0-0", "nonsense"} {
			if got := cache(what, other); got != "private, no-cache" {
				t.Errorf("%s with v=%q: %q", what, other, got)
			}
		}
	}

	// The file changes: its old URL is revalidated, the new listing names
	// another version, and that one is kept again.
	put(320, 240)
	if got := cache("thumbnail", v); got != "private, no-cache" {
		t.Errorf("thumbnail at the old version: %q", got)
	}
	v2 := listed()
	if v2 == v {
		t.Fatalf("the version did not change: %q", v)
	}
	if got := cache("thumbnail", v2); got != immutable {
		t.Errorf("thumbnail at the new version: %q", got)
	}
}
