package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, path string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// One app per language, each under its own path; a path without one goes
// to the language chosen, or asked for.
func TestLanguages(t *testing.T) {
	h := handler(fstest.MapFS{
		"en/index.html":       {Data: []byte("english")},
		"en/main-ABC.js":      {Data: []byte("en js")},
		"de/index.html":       {Data: []byte("deutsch")},
		"de/favicon.svg":      {Data: []byte("<svg/>")},
		"de/site.webmanifest": {Data: []byte("{}")},
		"3rdpartylicenses.x":  {Data: []byte("x")},
	})
	if w := get(t, h, "/de/browse/Family", nil); w.Body.String() != "deutsch" {
		t.Errorf("a route of the German app: %q", w.Body.String())
	}
	if w := get(t, h, "/en/main-ABC.js", nil); w.Body.String() != "en js" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("a bundle: %q %v", w.Body.String(), w.Header())
	}
	if w := get(t, h, "/de/site.webmanifest", nil); w.Header().Get("Content-Type") != "application/manifest+json" {
		t.Errorf("the manifest: %v", w.Header())
	}
	for _, c := range []struct {
		path   string
		header map[string]string
		want   string
	}{
		{"/", nil, "/en/"},
		{"/browse/Family?path=Fotos", map[string]string{"Accept-Language": "de-DE,de;q=0.9,en;q=0.8"}, "/de/browse/Family?path=Fotos"},
		{"/", map[string]string{"Accept-Language": "fr-FR, en;q=0.5, de;q=0.7"}, "/de/"},
		{"/", map[string]string{"Accept-Language": "de", "Cookie": Cookie + "=en"}, "/en/"},
		{"/", map[string]string{"Cookie": Cookie + "=fr"}, "/en/"},
	} {
		w := get(t, h, c.path, c.header)
		if w.Code != http.StatusFound || w.Header().Get("Location") != c.want {
			t.Errorf("%s %v: %d %s, want %s", c.path, c.header, w.Code, w.Header().Get("Location"), c.want)
		}
	}
}

// A build of one language only, at the top, is served as before.
func TestSingleApp(t *testing.T) {
	h := handler(fstest.MapFS{"index.html": {Data: []byte("app")}})
	if w := get(t, h, "/browse/alex", nil); w.Body.String() != "app" {
		t.Errorf("%d %q", w.Code, w.Body.String())
	}
	if w := get(t, h, "/", nil); w.Header().Get("Content-Security-Policy") == "" {
		t.Error("no Content-Security-Policy")
	}
}
