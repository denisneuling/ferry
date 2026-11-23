package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

// ndjson is the listing as it is read, one JSON object per line.
const ndjson = "application/x-ndjson"

// ListEntries writes a folder's entries out as the server yields them, a
// batch at a time, so that a folder of a hundred thousand names costs ferry
// one batch of memory and the interface sees the first names at once. The
// folder is opened and its first batch read before the status is written:
// what fails that early is an ordinary problem response.
func (a *API) ListEntries(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.ListEntriesParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolInteractive)
	if !ok {
		return
	}
	defer done()
	dir, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	at, ok := atParam(w, params.At)
	if !ok {
		return
	}
	var d *smb.Dir
	var err error
	if at.IsZero() {
		d, err = conn.OpenDir(ctx, share, dir, a.cfg.MaxListing)
	} else {
		d, err = conn.OpenDirAt(ctx, share, dir, a.cfg.MaxListing, at)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer func() { _ = d.Close() }()
	batch, err := d.Next()
	if err != nil && !errors.Is(err, io.EOF) {
		a.fail(w, r, err)
		return
	}

	out := listWriter{w: w, rc: http.NewResponseController(w), stall: a.cfg.StallTimeout, dir: dir, stream: wantsNDJSON(r)}
	out.begin()
	for {
		if werr := out.entries(batch); werr != nil {
			// The client is gone or stalled; there is no one to tell.
			return
		}
		if errors.Is(err, io.EOF) {
			_ = out.end()
			return
		}
		if batch, err = d.Next(); err != nil && !errors.Is(err, io.EOF) {
			a.failListing(r, &out, err)
			return
		}
	}
}

// failListing reports a listing that failed after it had begun. The status
// is written; a stream says so in its last line, and a JSON body is cut off
// by ending the connection, so that what arrived never parses as complete.
func (a *API) failListing(r *http.Request, out *listWriter, err error) {
	if smb.KindOf(err) == smb.KindSessionLost {
		// The cookie cannot be cleared any more; the session table forgets
		// the session, and the next request is a 401.
		a.sessions.Delete(sessionID(r))
	}
	a.metrics.SMBError(smb.KindOf(err).String())
	p := smbProblem(err)
	if p.status >= 500 {
		a.log.Warn("listing failed", append(errAttrs(err), "route", r.Pattern)...)
	}
	if out.stream {
		_ = out.problem(p)
		return
	}
	_ = out.bw.Flush()
	panic(http.ErrAbortHandler)
}

// wantsNDJSON is whether the client asked for the listing as a stream of
// lines rather than one JSON document.
func wantsNDJSON(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		if t, _, err := mime.ParseMediaType(strings.TrimSpace(part)); err == nil && t == ndjson {
			return true
		}
	}
	return false
}

// listWriter writes a listing in either representation, flushing after each
// batch under a write deadline: a client that stops reading ends the
// listing, not the session's slot, which Dir holds only per batch.
type listWriter struct {
	w      http.ResponseWriter
	rc     *http.ResponseController
	bw     *bufio.Writer
	enc    *json.Encoder
	stall  time.Duration
	dir    string
	stream bool
	count  int
}

func (l *listWriter) begin() {
	h := l.w.Header()
	h.Set("Cache-Control", "no-store")
	if l.stream {
		h.Set("Content-Type", ndjson)
	} else {
		h.Set("Content-Type", "application/json")
	}
	l.w.WriteHeader(http.StatusOK)
	l.bw = bufio.NewWriterSize(l.w, 32<<10)
	l.enc = json.NewEncoder(l.bw)
	if !l.stream {
		path, _ := json.Marshal(l.dir)
		_, _ = l.bw.WriteString(`{"path":` + string(path) + `,"entries":[`)
	}
}

func (l *listWriter) entries(batch []smb.Entry) error {
	for _, e := range batch {
		if !l.stream && l.count > 0 {
			_ = l.bw.WriteByte(',')
		}
		v := entry(l.dir, e)
		var err error
		if l.stream {
			err = l.enc.Encode(v) // Encode ends the line.
		} else {
			var b []byte
			if b, err = json.Marshal(v); err == nil {
				_, err = l.bw.Write(b)
			}
		}
		if err != nil {
			return err
		}
		l.count++
	}
	return l.flush()
}

func (l *listWriter) end() error {
	if l.stream {
		if err := l.enc.Encode(map[string]any{"end": map[string]int{"count": l.count}}); err != nil {
			return err
		}
	} else {
		_, _ = l.bw.WriteString("]}\n")
	}
	return l.flush()
}

func (l *listWriter) problem(p problem) error {
	if err := l.enc.Encode(map[string]v1.Problem{"problem": p.body()}); err != nil {
		return err
	}
	return l.flush()
}

func (l *listWriter) flush() error {
	_ = l.rc.SetWriteDeadline(time.Now().Add(l.stall))
	if err := l.bw.Flush(); err != nil {
		return err
	}
	return l.rc.Flush()
}
