package api

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

// onShare is the common start of every handler on a share: the session, the
// deployment's allow-list, and a context bounded by the request timeout. A
// share outside FERRY_SHARES is answered as if it did not exist.
// onShare finds the request's session and the connection of its group that
// serves pool (smb.PoolInteractive, smb.PoolBackground), and bounds the
// request's time. done ends it all.
func (a *API) onShare(w http.ResponseWriter, r *http.Request, share, pool string) (*smb.Session, context.Context, func(), bool) {
	s, ok := a.acquire(w, r)
	if !ok {
		return nil, nil, nil, false
	}
	if !a.cfg.AllowsShare(share) {
		a.sessions.Release(s)
		probNotFound.write(w)
		return nil, nil, nil, false
	}
	c, given, ok := connFor(s, pool)
	if !ok {
		a.sessions.Release(s)
		probInternal.write(w)
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	return c, ctx, func() { cancel(); given(); a.sessions.Release(s) }, true
}

func (a *API) ListShares(w http.ResponseWriter, r *http.Request) {
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	c, given, ok := connFor(s, smb.PoolInteractive)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	defer cancel()
	shares, err := c.Shares(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := v1.ShareList{Shares: []v1.Share{}}
	for _, sh := range shares {
		if a.cfg.AllowsShare(sh.Name) {
			out.Shares = append(out.Shares, v1.Share{Name: sh.Name, Comment: sh.Comment})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// spaceTimeout bounds the wait for an answer about free space: it is a
// courtesy line under a title, and a busy session should not hold it long.
const spaceTimeout = 5 * time.Second

func (a *API) GetSpace(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetSpaceParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolInteractive)
	if !ok {
		return
	}
	defer done()
	dir, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, spaceTimeout)
	defer cancel()
	sp, err := conn.Space(ctx, share, dir)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v1.Space{Total: clampInt64(sp.Total), Free: clampInt64(sp.Free)})
}

// clampInt64 keeps a server's absurd answer from turning negative in JSON.
func clampInt64(n uint64) int64 {
	if n > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(n)
}

func (a *API) CreateFolder(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.CreateFolderParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolInteractive)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	e, err := conn.Mkdir(ctx, share, p)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry(parent(p), e))
}

func (a *API) MoveEntry(w http.ResponseWriter, r *http.Request, share v1.ShareName, _ v1.MoveEntryParams) {
	var req v1.MoveRequest
	if !decodeJSON(w, r, 16<<10, &req) {
		return
	}
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolInteractive)
	if !ok {
		return
	}
	defer done()
	from, ok := cleanParam(w, &req.From)
	if !ok {
		return
	}
	to, ok := cleanParam(w, &req.To)
	if !ok {
		return
	}
	e, err := conn.Rename(ctx, share, from, to, req.Overwrite != nil && *req.Overwrite)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entry(parent(to), e))
}

func (a *API) DeleteEntry(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.DeleteEntryParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolInteractive)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	if err := conn.Remove(ctx, share, p, params.Recursive != nil && *params.Recursive, a.cfg.MaxDelete); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// cleanParam validates a path parameter and returns it in the API's own
// form: slash-separated, without leading or trailing slashes. internal/smb
// cleans it once more on its way to the server.
func cleanParam(w http.ResponseWriter, p *string) (string, bool) {
	var raw string
	if p != nil {
		raw = *p
	}
	sp, err := smb.CleanPath(raw)
	if err != nil {
		probInvalidPath.write(w)
		return "", false
	}
	return strings.ReplaceAll(sp, `\`, "/"), true
}

func parent(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

func entry(dir string, e smb.Entry) v1.Entry {
	p := e.Name
	if dir != "" {
		p = dir + "/" + e.Name
	}
	t := v1.EntryTypeFile
	if e.Dir {
		t = v1.EntryTypeFolder
	}
	return v1.Entry{Name: e.Name, Path: p, Type: t, Size: e.Size, Modified: e.ModTime.UTC(), Version: version(e.Size, e.ModTime)}
}
