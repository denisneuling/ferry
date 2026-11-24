package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

// usageTimeout bounds adding up one folder; the count by then is the answer.
const usageTimeout = 60 * time.Second

// usageEvery is how often the count so far is written while it grows.
const usageEvery = time.Second

// GetUsage adds up what a folder holds (internal/smb, usage.go) and writes
// the count as it grows. Like a search it runs on a background connection,
// one folder at a time, one per session, and keeps nothing.
func (a *API) GetUsage(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetUsageParams) {
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	if !a.cfg.AllowsShare(share) {
		probNotFound.write(w)
		return
	}
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	at, ok := atParam(w, params.At)
	if !ok {
		return
	}
	conn, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()
	// One per session: opening another folder's size ends this one.
	ctx, done := a.usages.begin(sessionID(r), r.Context())
	defer done()
	ctx, cancel := context.WithTimeout(ctx, usageTimeout)
	defer cancel()

	out := &usageWriter{w: w, rc: http.NewResponseController(w), stall: a.cfg.StallTimeout}
	lim := smb.DefaultUsageLimits
	lim.FolderEntries = a.cfg.MaxListing
	var last time.Time
	res, err := conn.Usage(ctx, share, p, at, lim, func(u smb.Usage) {
		if out.werr != nil || time.Since(last) < usageEvery {
			return
		}
		last = time.Now()
		if err := out.line(v1.UsageProgress{Progress: usage(u)}); err != nil {
			cancel() // the client is gone or stalled: stop before the next folder
		}
	})
	switch {
	case err == nil:
		_ = out.line(v1.UsageEnd{End: usage(res)})
	case out.werr != nil || smb.KindOf(err) == smb.KindCanceled:
		// No one left to tell.
	case !out.begun:
		a.fail(w, r, err)
	default:
		a.metrics.SMBError(smb.KindOf(err).String())
		if smb.KindOf(err) == smb.KindSessionLost {
			a.sessions.Delete(sessionID(r))
		}
		_ = out.line(map[string]v1.Problem{"problem": smbProblem(err).body()})
	}
}

func usage(u smb.Usage) v1.Usage {
	v := v1.Usage{Files: u.Files, Folders: u.Folders, Bytes: u.Bytes, Skipped: u.Skipped, Truncated: u.Truncated}
	if u.Reason != "" {
		r := v1.UsageReason(u.Reason)
		v.Reason = &r
	}
	return v
}

// usageWriter writes a folder size's lines, beginning the answer only with
// the first of them, so that an error before it is a problem response.
type usageWriter struct {
	w     http.ResponseWriter
	rc    *http.ResponseController
	bw    *bufio.Writer
	stall time.Duration
	begun bool
	werr  error
}

func (u *usageWriter) line(v any) error {
	if u.werr != nil {
		return u.werr
	}
	if !u.begun {
		u.begun = true
		h := u.w.Header()
		h.Set("Content-Type", ndjson)
		h.Set("Cache-Control", "no-store")
		u.w.WriteHeader(http.StatusOK)
		u.bw = bufio.NewWriterSize(u.w, 4<<10)
	}
	if err := json.NewEncoder(u.bw).Encode(v); err != nil {
		u.werr = err
		return err
	}
	_ = u.rc.SetWriteDeadline(time.Now().Add(u.stall))
	if err := u.bw.Flush(); err != nil {
		u.werr = err
		return err
	}
	if err := u.rc.Flush(); err != nil {
		u.werr = err
		return err
	}
	return nil
}
