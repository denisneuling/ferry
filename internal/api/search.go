package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

// searchTimeout bounds one search; what was found by then is the answer.
const searchTimeout = 60 * time.Second

// maxQuery bounds what a search looks for, in bytes.
const maxQuery = 256

// Search looks for names below a folder (internal/smb, search.go) and
// writes the hits out as they are found. It runs on a background
// connection, taking it only one folder at a time, so the interactive one
// stays free for browsing; nothing is kept, no index, no log of what was
// looked for.
func (a *API) Search(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.SearchParams) {
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
	q := strings.TrimSpace(params.Q)
	if q == "" || len(q) > maxQuery || !utf8.ValidString(q) {
		probInvalidRequest.write(w)
		return
	}
	conn, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()
	// One search per session: a new one — typing on, another tab — ends
	// the one running, which stops before its next folder.
	ctx, done := a.searches.begin(sessionID(r), r.Context())
	defer done()
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	start := time.Now()
	out := &searchWriter{w: w, rc: http.NewResponseController(w), stall: a.cfg.StallTimeout}
	lim := smb.DefaultSearchLimits
	lim.FolderEntries = a.cfg.MaxListing
	res, err := conn.Search(ctx, share, p, q, at, lim, out.hits)
	switch {
	case err == nil:
		outcome := "ok"
		if res.Truncated {
			outcome = "truncated"
		}
		a.metrics.Search(outcome, time.Since(start))
		_ = out.end(res)
	case out.werr != nil || smb.KindOf(err) == smb.KindCanceled:
		// The client is gone or stalled; there is no one to tell.
		a.metrics.Search("canceled", time.Since(start))
	case !out.begun:
		a.metrics.Search("failed", time.Since(start))
		a.fail(w, r, err)
	default:
		a.metrics.Search("failed", time.Since(start))
		a.metrics.SMBError(smb.KindOf(err).String())
		if smb.KindOf(err) == smb.KindSessionLost {
			a.sessions.Delete(sessionID(r))
		}
		_ = out.problem(smbProblem(err))
	}
}

// searches is the search each session has running, so that a new one can
// end it. A search takes its connection only one folder at a time (as a
// listing does), so thumbnails and downloads go in between; one per session
// keeps a person's searches from crowding out the rest of what they load.
type searches struct {
	mu      sync.Mutex
	running map[string]*searchRun
}

type searchRun struct{ cancel context.CancelFunc }

func newSearches() *searches { return &searches{running: map[string]*searchRun{}} }

// begin ends the session's running search, if any, and starts this one.
// done ends it and forgets it, unless a newer one took its place.
func (s *searches) begin(session string, parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	run := &searchRun{cancel: cancel}
	s.mu.Lock()
	if old := s.running[session]; old != nil {
		old.cancel()
	}
	s.running[session] = run
	s.mu.Unlock()
	return ctx, func() {
		cancel()
		s.mu.Lock()
		if s.running[session] == run {
			delete(s.running, session)
		}
		s.mu.Unlock()
	}
}

// searchWriter writes a search's lines, beginning the answer only with the
// first of them, so that an error before any hit is a problem response.
type searchWriter struct {
	w     http.ResponseWriter
	rc    *http.ResponseController
	bw    *bufio.Writer
	enc   *json.Encoder
	stall time.Duration
	begun bool
	werr  error
}

func (s *searchWriter) begin() {
	if s.begun {
		return
	}
	s.begun = true
	h := s.w.Header()
	h.Set("Content-Type", ndjson)
	h.Set("Cache-Control", "no-store")
	s.w.WriteHeader(http.StatusOK)
	s.bw = bufio.NewWriterSize(s.w, 16<<10)
	s.enc = json.NewEncoder(s.bw)
}

func (s *searchWriter) hits(dir string, hits []smb.Entry) error {
	s.begin()
	for _, e := range hits {
		if err := s.enc.Encode(v1.SearchHit{Hit: entry(dir, e)}); err != nil {
			s.werr = err
			return err
		}
	}
	if err := s.flush(); err != nil {
		s.werr = err
		return err
	}
	return nil
}

func (s *searchWriter) end(res smb.SearchResult) error {
	s.begin()
	var e v1.SearchEnd
	e.End.Hits, e.End.Folders, e.End.Skipped, e.End.Truncated = res.Hits, res.Folders, res.Skipped, res.Truncated
	if res.Reason != "" {
		r := v1.SearchEndEndReason(res.Reason)
		e.End.Reason = &r
	}
	if err := s.enc.Encode(e); err != nil {
		return err
	}
	return s.flush()
}

func (s *searchWriter) problem(p problem) error {
	if err := s.enc.Encode(map[string]v1.Problem{"problem": p.body()}); err != nil {
		return err
	}
	return s.flush()
}

func (s *searchWriter) flush() error {
	_ = s.rc.SetWriteDeadline(time.Now().Add(s.stall))
	if err := s.bw.Flush(); err != nil {
		return err
	}
	return s.rc.Flush()
}
