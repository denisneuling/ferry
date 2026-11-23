package api

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/session"
	"github.com/denisneuling/ferry/internal/smb"
)

// A ZIP download takes two requests. A browser downloads a file to disk only
// by navigating to it, and a navigation carries no X-Ferry-Request header and
// no body: so the POST, which has both, lists what goes into the archive and
// answers with a ticket, and the GET of the ticket streams the archive.
const (
	// archiveTicketLife is how long a ticket waits for its download.
	archiveTicketLife = time.Minute
	// archiveTicketsPerSession bounds the tickets one session holds; a new
	// one beyond it pushes out its oldest.
	archiveTicketsPerSession = 4
	// archiveTicketEntries bounds the entries all tickets hold together,
	// some 20 MB: beyond it the oldest tickets, whoever's, go first.
	archiveTicketEntries = 200_000
	// archiveRequestBytes bounds the POST's body: the paths of a large
	// selection, a few hundred bytes each.
	archiveRequestBytes = 4 << 20
)

// archiveTicket is a listed archive waiting for its download: in memory
// only, for one session, once.
type archiveTicket struct {
	owner   *session.Session
	share   string
	base    string // the folder the entries are in, slash-separated
	name    string
	entries []smb.TreeEntry
	created time.Time
}

type archiveTickets struct {
	mu sync.Mutex
	m  map[string]*archiveTicket
}

// put keeps t under a new ticket ID, dropping expired tickets and the
// owner's oldest beyond the bound.
func (at *archiveTickets) put(t *archiveTicket) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	at.mu.Lock()
	defer at.mu.Unlock()
	if at.m == nil {
		at.m = map[string]*archiveTicket{}
	}
	var mine []string
	for k, v := range at.m {
		switch {
		case time.Since(v.created) > archiveTicketLife:
			delete(at.m, k)
		case v.owner == t.owner:
			mine = append(mine, k)
		}
	}
	for len(mine) >= archiveTicketsPerSession {
		oldest := 0
		for i, k := range mine {
			if at.m[k].created.Before(at.m[mine[oldest]].created) {
				oldest = i
			}
		}
		delete(at.m, mine[oldest])
		mine = append(mine[:oldest], mine[oldest+1:]...)
	}
	at.m[id] = t
	total := 0
	for _, v := range at.m {
		total += len(v.entries)
	}
	for total > archiveTicketEntries && len(at.m) > 1 {
		oldest := ""
		for k, v := range at.m {
			if k != id && (oldest == "" || v.created.Before(at.m[oldest].created)) {
				oldest = k
			}
		}
		total -= len(at.m[oldest].entries)
		delete(at.m, oldest)
	}
	return id
}

// take hands out the ticket once, to its owner, within its life.
func (at *archiveTickets) take(id string, owner *session.Session, share string) (*archiveTicket, bool) {
	at.mu.Lock()
	defer at.mu.Unlock()
	t, ok := at.m[id]
	if !ok || t.owner != owner || t.share != share {
		return nil, false
	}
	delete(at.m, id)
	return t, time.Since(t.created) <= archiveTicketLife
}

func (a *API) CreateArchive(w http.ResponseWriter, r *http.Request, share v1.ShareName, _ v1.CreateArchiveParams) {
	var req v1.ArchiveRequest
	if !decodeJSON(w, r, archiveRequestBytes, &req) {
		return
	}
	if len(req.Paths) == 0 || len(req.Paths) > 10000 {
		probInvalidRequest.write(w)
		return
	}
	// Every entry in the same folder, each once: the archive holds them
	// under their names in it.
	paths := make([]string, 0, len(req.Paths))
	seen := map[string]bool{}
	base := ""
	for i, raw := range req.Paths {
		p, ok := cleanParam(w, &raw)
		if !ok {
			return
		}
		if p == "" {
			probInvalidPath.write(w)
			return
		}
		dir := parent(p)
		if i == 0 {
			base = dir
		} else if dir != base {
			probInvalidRequest.write(w)
			return
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	if len(paths) > a.cfg.MaxArchive {
		p := probTooLarge
		p.limit = int64(a.cfg.MaxArchive)
		p.write(w)
		return
	}
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolInteractive)
	if !ok {
		return
	}
	defer done()
	entries, err := conn.Tree(ctx, share, paths, a.cfg.MaxArchive)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var bytes int64
	for _, e := range entries {
		bytes += e.Size
	}
	name := path.Base("/" + base)
	if len(paths) == 1 {
		name = path.Base("/" + paths[0])
	} else if base == "" {
		name = share
	}
	t := &archiveTicket{owner: s, share: share, base: base, name: name + ".zip", entries: entries, created: time.Now()}
	id := a.archives.put(t)
	writeJSON(w, http.StatusCreated, v1.ArchiveTicket{Ticket: id, Name: t.name, Entries: len(entries), Bytes: bytes})
}

func (a *API) GetArchive(w http.ResponseWriter, r *http.Request, share v1.ShareName, ticket string) {
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	t, ok := a.archives.take(ticket, s, share)
	if !ok || !a.cfg.AllowsShare(share) {
		probNotFound.write(w)
		return
	}
	conn, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()

	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", disposition("attachment", t.name))
	h.Set("Content-Security-Policy", contentCSP)
	w.WriteHeader(http.StatusOK)

	// Each file is opened within the request timeout and read within the
	// stall timeout per chunk, as a download is (content.go).
	wd := newWatchdog(r.Context(), a.cfg.RequestTimeout)
	defer wd.stop()
	out := &stallWriter{w: w, rc: http.NewResponseController(w), stall: a.cfg.StallTimeout}
	zw := zip.NewWriter(out)
	buf := make([]byte, smb.StreamChunk)
	var files int64
	for _, e := range t.entries {
		hdr := &zip.FileHeader{Name: zipName(t.base, e.Path), Method: zip.Store, Modified: e.ModTime}
		if e.Dir {
			hdr.Name += "/"
			if _, err := zw.CreateHeader(hdr); err != nil {
				a.abortArchive(r, err, out.n)
			}
			continue
		}
		hdr.UncompressedSize64 = uint64(e.Size)
		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			a.abortArchive(r, err, out.n)
		}
		wd.kick(a.cfg.RequestTimeout)
		f, err := conn.Open(wd.ctx, share, e.Path)
		if err != nil {
			a.abortArchive(r, err, out.n)
		}
		n, err := copyChunks(dst, f, buf, wd, a.cfg.StallTimeout)
		_ = f.Close()
		files += n
		if err != nil {
			a.abortArchive(r, err, out.n)
		}
	}
	if err := zw.Close(); err != nil {
		a.abortArchive(r, err, out.n)
	}
	a.metrics.Down(files)
	a.metrics.Archive("ok")
}

// zipName is an entry's name in the archive: its path below base. A
// backslash a server let into a name would be a folder separator to
// Windows' Explorer and to some unpackers, which would put the file
// somewhere else than its name says; it becomes an underscore.
func zipName(base, p string) string {
	if base != "" {
		p = strings.TrimPrefix(p, base+"/")
	}
	return strings.ReplaceAll(p, `\`, "_")
}

// abortArchive ends an archive that failed once begun. The status went out
// with the first byte; closing the connection is what is left, so that the
// browser reports a failed download rather than keep an archive that looks
// complete with a file missing. It does not return.
func (a *API) abortArchive(r *http.Request, err error, sent int64) {
	outcome := "failed"
	if r.Context().Err() != nil || smb.KindOf(err) == smb.KindCanceled {
		outcome = "canceled"
	}
	a.metrics.Archive(outcome)
	a.metrics.Down(sent)
	if smb.KindOf(err) == smb.KindSessionLost && a.groupLost(r) {
		a.sessions.Delete(sessionID(r))
	}
	a.log.Info("archive ended early", append(errAttrs(err), "outcome", outcome, "bytes", sent)...)
	panic(http.ErrAbortHandler)
}

// copyChunks copies f to w in reads of len(buf), each within stall.
func copyChunks(w io.Writer, f io.Reader, buf []byte, wd *watchdog, stall time.Duration) (int64, error) {
	var n int64
	for {
		wd.kick(stall)
		nr, rerr := f.Read(buf)
		if nr > 0 {
			nw, werr := w.Write(buf[:nr])
			n += int64(nw)
			if werr != nil {
				return n, werr
			}
		}
		if rerr == io.EOF {
			return n, nil
		}
		if rerr != nil {
			return n, rerr
		}
	}
}

// stallWriter writes to the response with a write deadline of stall per
// write, so that a client that stops reading ends the archive, and counts
// what went out.
type stallWriter struct {
	w     io.Writer
	rc    *http.ResponseController
	stall time.Duration
	n     int64
}

func (s *stallWriter) Write(p []byte) (int, error) {
	_ = s.rc.SetWriteDeadline(time.Now().Add(s.stall))
	n, err := s.w.Write(p)
	s.n += int64(n)
	return n, err
}
