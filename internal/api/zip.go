package api

import (
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
	"github.com/denisneuling/ferry/internal/zipread"
)

// ListZip answers with a ZIP file's table of contents (internal/zipread):
// read from its central directory as the file lies on the share, under
// the person's session; nothing is unpacked or written.
func (a *API) ListZip(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.ListZipParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolBackground)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	at, ok := atParam(w, params.At)
	if !ok {
		return
	}
	f, again, err := a.openDerived(ctx, r, conn, share, p, at)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer again()
	defer func() { _ = f.Close() }()
	es, err := zipread.List(f, f.Size, a.cfg.MaxListing)
	if a.zipFailed(w, r, err) {
		return
	}
	out := v1.ZipList{Entries: make([]v1.ZipEntry, 0, len(es))}
	for _, e := range es {
		t := v1.EntryTypeFile
		if e.Dir {
			t = v1.EntryTypeFolder
		}
		z := v1.ZipEntry{Name: e.Name, Type: t, Size: e.Size, Packed: e.Packed, Readable: e.Readable()}
		if !e.Modified.IsZero() {
			m := e.Modified.UTC()
			z.Modified = &m
		}
		out.Entries = append(out.Entries, z)
	}
	writeJSON(w, http.StatusOK, out)
}

// GetZipEntry sends one entry out of a ZIP file, unpacked as it streams and
// never more than its header declares, like a download: attachment under
// its base name, or inline under the same sandbox as a file's content.
func (a *API) GetZipEntry(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetZipEntryParams) {
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	if !a.cfg.AllowsShare(share) {
		probNotFound.write(w)
		return
	}
	conn, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	at, ok := atParam(w, params.At)
	if !ok {
		return
	}
	// Finding the entry is bounded by the request timeout; the stream
	// afterwards only by the stall timeout, as a download is.
	wd := newWatchdog(r.Context(), a.cfg.RequestTimeout)
	defer wd.stop()
	f, err := openFile(wd.ctx, conn, share, p, at)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()
	es, err := zipread.List(f, f.Size, a.cfg.MaxListing)
	if a.zipFailed(w, r, err) {
		return
	}
	var e zipread.Entry
	found := false
	for _, c := range es {
		if c.Name == params.Entry && !c.Dir {
			e, found = c, true
			break
		}
	}
	if !found {
		probNotFound.write(w)
		return
	}
	if !e.Readable() {
		probNotAnArchive.write(w)
		return
	}
	rd, err := zipread.Open(f, f.Size, e, 0)
	if a.zipFailed(w, r, err) {
		return
	}
	name := entryBase(e.Name)
	ctype, inline := contentType(name, params.Disposition != nil && *params.Disposition == v1.GetZipEntryParamsDispositionInline)
	kind := "attachment"
	if inline {
		kind = "inline"
	}
	h := w.Header()
	h.Set("Content-Security-Policy", contentCSP)
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", disposition(kind, name))
	h.Set("Content-Length", strconv.FormatInt(e.Size, 10))
	h.Set("Cache-Control", "private, no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	n, err := copyOut(w, rd, e.Size, wd, a.cfg.StallTimeout)
	a.metrics.Down(n)
	if err != nil && n < e.Size {
		// A damaged entry, or one that unpacks into more than it says, is
		// broken off: the client sees a short body.
		a.log.Info("zip entry ended early", append(errAttrs(err), "bytes", n)...)
	}
}

// zipFailed answers for an archive that could not be read, and says
// whether it did.
func (a *API) zipFailed(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, zipread.ErrTooLarge):
		p := probTooLarge
		p.limit = int64(a.cfg.MaxListing)
		p.write(w)
	case errors.Is(err, zipread.ErrNotZip), errors.Is(err, zipread.ErrUnsupported), errors.Is(err, zipread.ErrCorrupt):
		probNotAnArchive.write(w)
	default:
		a.fail(w, r, err)
	}
	return true
}

// entryBase is the name a download of an entry gets: the last part of its
// path in the archive — either separator — without control characters,
// and never empty.
func entryBase(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	b := path.Base("/" + name)
	b = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, b)
	if b == "" || b == "/" || b == "." || b == ".." {
		return "entry"
	}
	return b
}
