package api

import (
	"context"
	"net/http"
	"strings"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/smb"
)

// onShare is the common start of every handler on a share: the session, the
// deployment's allow-list, and a context bounded by the request timeout. A
// share outside FERRY_SHARES is answered as if it did not exist.
func (a *API) onShare(w http.ResponseWriter, r *http.Request, share string) (*smb.Session, context.Context, func(), bool) {
	s, ok := a.acquire(w, r)
	if !ok {
		return nil, nil, nil, false
	}
	if !a.cfg.AllowsShare(share) {
		a.sessions.Release(s)
		probNotFound.write(w)
		return nil, nil, nil, false
	}
	conn, ok := s.Conn.(*smb.Session)
	if !ok {
		a.sessions.Release(s)
		probInternal.write(w)
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	return conn, ctx, func() { cancel(); a.sessions.Release(s) }, true
}

func (a *API) ListShares(w http.ResponseWriter, r *http.Request) {
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	conn, ok := s.Conn.(*smb.Session)
	if !ok {
		probInternal.write(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	defer cancel()
	shares, err := conn.Shares(ctx)
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

func (a *API) CreateFolder(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.CreateFolderParams) {
	conn, ctx, done, ok := a.onShare(w, r, share)
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
	conn, ctx, done, ok := a.onShare(w, r, share)
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
	conn, ctx, done, ok := a.onShare(w, r, share)
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
	return v1.Entry{Name: e.Name, Path: p, Type: t, Size: e.Size, Modified: e.ModTime.UTC()}
}
