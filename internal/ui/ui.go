// Package ui serves the interface, built by `make ui-build` into dist/app
// and compiled into the binary. A path that is no file is the interface's own
// route: it gets index.html, and the interface's router takes it from there.
//
// The interface is built once per language (@angular/localize), as faber's:
// dist/app/en/, dist/app/de/, each a whole application under its own path. A
// path without a language goes to the one the person chose before (the
// ferry-lang cookie, set by the interface), else the one the browser asks
// for, else English — with its query.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Cookie remembers the language the user chose.
const Cookie = "ferry-lang"

// Built says whether the interface was built into this binary.
func Built() bool {
	files, err := fs.Sub(dist, "dist/app")
	if err != nil {
		return false
	}
	if _, err := fs.Stat(files, "index.html"); err == nil {
		return true
	}
	return len(languages(files)) > 0
}

// csp keeps the interface to itself: its own scripts, styles and fonts, and
// the API. Images, video, audio and the PDF frame load from ferry's content
// endpoint, which sends every file sandboxed (internal/api). Angular sets
// styles inline on elements, hence 'unsafe-inline' for styles only; no
// script runs that is not one of the build's files.
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; media-src 'self' blob:; frame-src 'self'; connect-src 'self'; " +
	"font-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// Handler serves the interface.
func Handler() http.Handler {
	files, err := fs.Sub(dist, "dist/app")
	if err != nil {
		panic(err)
	}
	return handler(files)
}

// handler serves the app in files: one per language in a directory of its
// own, or a single one at the top.
func handler(files fs.FS) http.Handler {
	langs := languages(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if len(langs) == 0 {
			serve(w, r, files, name)
			return
		}
		lang, rest, _ := strings.Cut(name, "/")
		if !contains(langs, lang) {
			// No language in the path: the preferred one's, same path.
			h.Set("Vary", "Accept-Language, Cookie")
			target := "/" + preferred(r, langs) + "/" + name
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		sub, err := fs.Sub(files, lang)
		if err != nil {
			http.Error(w, "app unavailable", http.StatusInternalServerError)
			return
		}
		serve(w, r, sub, rest)
	})
}

// serve sends the file name of the app in files, or — anything else being
// a route of the single-page app — its index.html.
func serve(w http.ResponseWriter, r *http.Request, files fs.FS, name string) {
	h := w.Header()
	if name != "" && name != "index.html" {
		if st, err := fs.Stat(files, name); err == nil && !st.IsDir() {
			// Angular names its bundles by their content.
			if strings.Contains(name, "-") && (strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".css")) {
				h.Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			if strings.HasSuffix(name, ".webmanifest") {
				h.Set("Content-Type", "application/manifest+json")
			}
			http.ServeFileFS(w, r, files, name)
			return
		}
	}
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		http.Error(w, "the interface is not built into this binary: run make ui-build", http.StatusNotFound)
		return
	}
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}

// languages are the directories of files that hold an app.
func languages(files fs.FS) []string {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := fs.Stat(files, e.Name()+"/index.html"); err == nil {
			out = append(out, e.Name())
		}
	}
	return out
}

// preferred is the language of langs the user chose before, else the one
// their browser ranks highest, else English, else the first.
func preferred(r *http.Request, langs []string) string {
	if c, err := r.Cookie(Cookie); err == nil && contains(langs, c.Value) {
		return c.Value
	}
	type ranked struct {
		lang string
		q    float64
	}
	var asked []ranked
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		base, _, _ := strings.Cut(strings.ToLower(tag), "-")
		if contains(langs, base) && q > 0 {
			asked = append(asked, ranked{base, q})
		}
	}
	sort.SliceStable(asked, func(i, j int) bool { return asked[i].q > asked[j].q })
	if len(asked) > 0 {
		return asked[0].lang
	}
	if contains(langs, "en") {
		return "en"
	}
	return langs[0]
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
