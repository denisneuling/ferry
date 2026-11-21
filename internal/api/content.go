package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/smb"
)

// contentCSP is the policy of every file ferry serves. `sandbox` puts the
// document in a unique origin with scripts, forms and plugins off, so even a
// file the browser renders cannot act in ferry's origin.
//
// frame-ancestors 'self' lets ferry's own interface show a PDF in a frame;
// no other site may frame a file.
const contentCSP = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'self'"

// inlineTypes are the media types a browser may show itself, with
// disposition=inline. Anything not listed — HTML, SVG, XML, scripts — goes as
// an attachment. Text of any kind is shown as text/plain.
var inlineTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
	"image/avif": true, "image/bmp": true,
	"video/mp4": true, "video/webm": true, "video/ogg": true, "video/quicktime": true,
	"audio/mpeg": true, "audio/ogg": true, "audio/wav": true, "audio/flac": true,
	"audio/mp4": true, "audio/aac": true, "audio/webm": true, "audio/x-wav": true,
	"application/pdf": true,
}

// textExtensions are shown inline as plain text: a preview of the source, never
// the rendered document.
var textExtensions = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".log": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".conf": true,
	".xml": true, ".html": true, ".htm": true, ".svg": true, ".css": true, ".js": true,
	".go": true, ".py": true, ".sh": true, ".c": true, ".h": true, ".java": true, ".ts": true,
}

// contentType decides what a file is served as. Attachments carry their
// real type (or octet-stream); inline answers only a type from inlineTypes,
// or text/plain for text, and fall back to an attachment otherwise.
func contentType(name string, inline bool) (ctype string, asInline bool) {
	ext := strings.ToLower(path.Ext(name))
	t := mime.TypeByExtension(ext)
	if mt, _, err := mime.ParseMediaType(t); err == nil {
		t = mt
	} else {
		t = ""
	}
	if inline {
		if inlineTypes[t] {
			return t, true
		}
		if textExtensions[ext] {
			return "text/plain; charset=utf-8", true
		}
	}
	if t == "" || strings.HasPrefix(t, "text/html") || t == "image/svg+xml" || strings.Contains(t, "xml") || strings.Contains(t, "javascript") {
		// Active types are never sent with their own type, even as an
		// attachment: a browser that ignored the disposition would render
		// them.
		return "application/octet-stream", false
	}
	return t, false
}

// disposition is a Content-Disposition header for a file name: an ASCII
// fallback and the exact name in RFC 5987 form.
func disposition(kind, name string) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == '%' || r == ';' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, kind, fallback, url.PathEscape(name))
}

func etag(size int64, mod time.Time) string {
	return fmt.Sprintf(`"%x-%x"`, size, mod.UnixNano())
}

func (a *API) GetContent(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetContentParams) {
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	if !a.cfg.AllowsShare(share) {
		probNotFound.write(w)
		return
	}
	conn, ok := s.Conn.(*smb.Session)
	if !ok {
		probInternal.write(w)
		return
	}
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	// Opening is bounded by the request timeout; the copy afterwards only by
	// the stall timeout, since a large file legitimately takes long. One
	// watchdog does both (watchdog.go).
	wd := newWatchdog(r.Context(), a.cfg.RequestTimeout)
	defer wd.stop()
	f, err := conn.Open(wd.ctx, share, p)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()

	tag := etag(f.Size, f.ModTime)
	name := path.Base("/" + p)
	ctype, inline := contentType(name, params.Disposition != nil && *params.Disposition == v1.GetContentParamsDispositionInline)
	kind := "attachment"
	if inline {
		kind = "inline"
	}
	h := w.Header()
	h.Set("Content-Security-Policy", contentCSP)
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", disposition(kind, name))
	h.Set("ETag", tag)
	h.Set("Last-Modified", f.ModTime.UTC().Format(http.TimeFormat))
	h.Set("Accept-Ranges", "bytes")
	// Private: the answer depends on who asks. Revalidated with the ETag.
	h.Set("Cache-Control", "private, no-cache")

	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	start, length := int64(0), f.Size
	status := http.StatusOK
	if rg := r.Header.Get("Range"); rg != "" && ifRangeHolds(r.Header.Get("If-Range"), tag, f.ModTime) {
		st, ln, ok, satisfiable := parseRange(rg, f.Size)
		switch {
		case !satisfiable:
			h.Set("Content-Range", fmt.Sprintf("bytes */%d", f.Size))
			probRangeNotSatisfied.write(w)
			return
		case ok:
			start, length, status = st, ln, http.StatusPartialContent
			h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", st, st+ln-1, f.Size))
		}
	}
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	h.Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	n, err := copyOut(w, f, length, wd, a.cfg.StallTimeout)
	if err != nil && n < length {
		// The status is sent; all that is left is to stop. The client sees
		// a short body against its Content-Length.
		a.log.Info("download ended early", append(errAttrs(err), "bytes", n)...)
	}
}

// copyOut writes n bytes of f to w in reads of smb.StreamChunk. net/http's
// own copy reads 32 KiB at a time, which is a quarter of the throughput
// (docs/spike.md). Every chunk must move within stall, or the download ends:
// the watchdog ends a read the SMB server does not answer, the write
// deadline a write the client does not take.
func copyOut(w http.ResponseWriter, f io.Reader, n int64, wd *watchdog, stall time.Duration) (int64, error) {
	rc := http.NewResponseController(w)
	buf := make([]byte, smb.StreamChunk)
	var done int64
	for done < n {
		want := min(int64(len(buf)), n-done)
		wd.kick(stall)
		nr, rerr := io.ReadFull(f, buf[:want])
		if nr > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(stall))
			nw, werr := w.Write(buf[:nr])
			done += int64(nw)
			if werr != nil {
				return done, werr
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.ErrUnexpectedEOF) || errors.Is(rerr, io.EOF) {
				// The file shrank while it was read.
				return done, io.ErrUnexpectedEOF
			}
			return done, rerr
		}
	}
	_ = rc.SetWriteDeadline(time.Time{})
	return done, nil
}

// parseRange reads a Range header for one range of a file of size bytes.
// ok is false for anything it does not serve as a range — several ranges, a
// unit other than bytes, a malformed header — which is then answered with
// the whole file, as RFC 9110 allows. satisfiable is false only for a single
// well-formed range that lies outside the file.
func parseRange(h string, size int64) (start, length int64, ok, satisfiable bool) {
	spec, found := strings.CutPrefix(h, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false, true
	}
	first, last, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false, true
	}
	if first == "" {
		// The last N bytes.
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, false, true
		}
		if n == 0 || size == 0 {
			return 0, 0, false, false
		}
		n = min(n, size)
		return size - n, n, true, true
	}
	s, err := strconv.ParseInt(first, 10, 64)
	if err != nil || s < 0 {
		return 0, 0, false, true
	}
	if s >= size {
		return 0, 0, false, false
	}
	e := size - 1
	if last != "" {
		e, err = strconv.ParseInt(last, 10, 64)
		if err != nil || e < s {
			return 0, 0, false, true
		}
		e = min(e, size-1)
	}
	return s, e - s + 1, true, true
}

func etagMatches(header, tag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == tag {
			return true
		}
	}
	return false
}

// ifRangeHolds says whether a Range may be honoured under If-Range: without
// one, or when it names this ETag (strongly) or this modification time.
func ifRangeHolds(h, tag string, mod time.Time) bool {
	if h == "" {
		return true
	}
	if strings.HasPrefix(h, `"`) {
		return h == tag
	}
	t, err := http.ParseTime(h)
	return err == nil && !mod.Truncate(time.Second).After(t)
}

func (a *API) PutContent(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.PutContentParams) {
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	if !a.cfg.AllowsShare(share) {
		probNotFound.write(w)
		return
	}
	conn, ok := s.Conn.(*smb.Session)
	if !ok {
		probInternal.write(w)
		return
	}
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	if a.cfg.MaxUpload > 0 && r.ContentLength > a.cfg.MaxUpload {
		probTooLarge.write(w)
		return
	}
	wd := newWatchdog(r.Context(), a.cfg.RequestTimeout)
	defer wd.stop()
	body := &stallReader{r: r.Body, rc: http.NewResponseController(w), wd: wd, stall: a.cfg.StallTimeout}
	e, err := conn.Upload(wd.ctx, share, p, body, params.Overwrite != nil && *params.Overwrite, a.cfg.MaxUpload)
	if err != nil {
		if smb.KindOf(err) == smb.KindLimit {
			// Read no more of an oversized body, and tell the client so.
			w.Header().Set("Connection", "close")
		}
		a.fail(w, r, err)
		return
	}
	drain(r.Body)
	writeJSON(w, http.StatusCreated, entry(parent(p), e))
}

// stallReader renews the connection's read deadline and the upload's
// watchdog before every read of the body: a client that stops sending, or an
// SMB server that stops taking the writes in between, ends the upload within
// stall, however long the whole upload may take.
type stallReader struct {
	r     io.Reader
	rc    *http.ResponseController
	wd    *watchdog
	stall time.Duration
}

func (s *stallReader) Read(p []byte) (int, error) {
	s.wd.kick(s.stall)
	_ = s.rc.SetReadDeadline(time.Now().Add(s.stall))
	return s.r.Read(p)
}
