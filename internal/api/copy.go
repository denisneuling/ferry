package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

// copyProgressEvery is how often a copy says how far it got: often enough
// that no proxy in front takes a long copy for a dead request.
const copyProgressEvery = time.Second

// CopyEntry copies a file or a folder within a share (internal/smb,
// copy.go), from the share as it is now or from a snapshot — a previous
// version restored as a copy. What would be copied is listed and bounded
// first, so that what is refused is a problem response before anything is
// written; then the copy runs and says how far it got, as NDJSON, until its
// last line. It is not bounded by the request timeout: a large copy takes
// long, and a client that leaves stops it between two chunks.
func (a *API) CopyEntry(w http.ResponseWriter, r *http.Request, share v1.ShareName, _ v1.CopyEntryParams) {
	var req v1.CopyRequest
	if !decodeJSON(w, r, 16<<10, &req) {
		return
	}
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	if !a.cfg.AllowsShare(share) {
		probNotFound.write(w)
		return
	}
	from, ok := cleanParam(w, &req.From)
	if !ok {
		return
	}
	to, ok := cleanParam(w, &req.To)
	if !ok {
		return
	}
	at, ok := atParam(w, req.At)
	if !ok {
		return
	}
	if from == "" || to == "" {
		probInvalidPath.write(w)
		return
	}
	if to == from || strings.HasPrefix(to, from+"/") {
		probCopyIntoItself.write(w)
		return
	}
	overwrite := req.Overwrite != nil && *req.Overwrite
	conn, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()

	// The plan, under the request timeout: listing a folder is a request
	// like any other.
	pctx, pcancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	plan, err := conn.CopyPlan(pctx, share, from, at, a.cfg.MaxArchive)
	if err == nil {
		// What is at the target is refused here, before anything is
		// written: a folder is never copied over anything, and a file
		// only over a file, with overwrite.
		switch target, serr := conn.Stat(pctx, share, to); {
		case serr == nil && (plan[0].Dir || !overwrite && !target.Dir):
			err = &smb.Error{Kind: smb.KindExists}
		case serr == nil && target.Dir:
			err = &smb.Error{Kind: smb.KindIsDir}
		case serr != nil && smb.KindOf(serr) != smb.KindNotFound:
			err = serr
		}
	}
	pcancel()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var total v1.CopyProgress
	for _, it := range plan {
		total.TotalFiles++
		total.TotalBytes += it.Size
	}
	if a.cfg.MaxUpload > 0 && total.TotalBytes > a.cfg.MaxUpload {
		smbProblem(&smb.Error{Kind: smb.KindLimit, Limit: a.cfg.MaxUpload}).write(w)
		return
	}

	out := &copyWriter{w: w, rc: http.NewResponseController(w), stall: a.cfg.StallTimeout, p: total, last: time.Now()}
	out.begin()
	ctx := r.Context()
	progress := func(n int64) {
		out.p.Bytes += n
		out.tick()
	}
	var made smb.Entry
	for i, it := range plan {
		src, dst := joinPath(from, it.Rel), joinPath(to, it.Rel)
		var e smb.Entry
		if it.Dir {
			e, err = conn.Mkdir(ctx, share, dst)
		} else {
			e, err = conn.CopyFile(ctx, share, src, dst, at, overwrite && len(plan) == 1, progress)
		}
		if err != nil {
			break
		}
		if i == 0 {
			made = e
		}
		out.p.Files++
		out.tick()
	}
	switch {
	case err == nil:
		a.metrics.Copy("ok")
		_ = out.end(entry(parent(to), made))
	case smb.KindOf(err) == smb.KindCanceled || out.werr != nil:
		a.metrics.Copy("canceled")
	default:
		a.metrics.Copy("failed")
		a.metrics.SMBError(smb.KindOf(err).String())
		if smb.KindOf(err) == smb.KindSessionLost {
			a.sessions.Delete(sessionID(r))
		}
		_ = out.problem(smbProblem(err))
	}
}

// joinPath puts a copy item's backslash-separated path below an API path.
func joinPath(base, rel string) string {
	if rel == "" {
		return base
	}
	return base + "/" + strings.ReplaceAll(rel, `\`, "/")
}

// copyWriter writes a copy's lines: progress about every second, then the
// last line.
type copyWriter struct {
	w     http.ResponseWriter
	rc    *http.ResponseController
	bw    *bufio.Writer
	enc   *json.Encoder
	stall time.Duration
	p     v1.CopyProgress
	last  time.Time
	werr  error
}

func (c *copyWriter) begin() {
	h := c.w.Header()
	h.Set("Content-Type", ndjson)
	h.Set("Cache-Control", "no-store")
	c.w.WriteHeader(http.StatusOK)
	c.bw = bufio.NewWriterSize(c.w, 4<<10)
	c.enc = json.NewEncoder(c.bw)
	_ = c.write(v1.CopyProgressLine{Progress: c.p}) // a failed write is kept in werr
}

// tick writes the progress when a second has passed since the last line.
func (c *copyWriter) tick() {
	if c.werr != nil || time.Since(c.last) < copyProgressEvery {
		return
	}
	_ = c.write(v1.CopyProgressLine{Progress: c.p}) // a failed write is kept in werr
}

func (c *copyWriter) end(e v1.Entry) error {
	var line v1.CopyEnd
	line.End.Entry, line.End.Progress = e, c.p
	return c.write(line)
}

func (c *copyWriter) problem(p problem) error {
	return c.write(v1.CopyProblem{Problem: p.body(), Progress: c.p})
}

func (c *copyWriter) write(v any) error {
	c.last = time.Now()
	if c.werr != nil {
		return c.werr
	}
	_ = c.rc.SetWriteDeadline(time.Now().Add(c.stall))
	if err := c.enc.Encode(v); err != nil {
		c.werr = err
		return err
	}
	if err := c.bw.Flush(); err != nil {
		c.werr = err
		return err
	}
	if err := c.rc.Flush(); err != nil {
		c.werr = err
	}
	return c.werr
}
