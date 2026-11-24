package api

import (
	"net/http"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

// maxPiece bounds one piece of an upload in pieces.
const maxPiece = 64 << 20

// An upload in pieces (internal/smb, partial.go): begun, written piece by
// piece, asked how far it has come, finished or given up. Each is a
// request of its own on a background connection, like a whole upload; the
// partial file on the share is all there is of it.

func (a *API) BeginUpload(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.BeginUploadParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolBackground)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	id, err := conn.PartialBegin(ctx, share, p, params.Overwrite != nil && *params.Overwrite)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v1.UploadBegun{Id: id})
}

func (a *API) GetUpload(w http.ResponseWriter, r *http.Request, share v1.ShareName, id v1.UploadId, params v1.GetUploadParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolBackground)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	n, err := conn.PartialSize(ctx, share, p, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v1.UploadState{Size: n})
}

func (a *API) PutUploadPiece(w http.ResponseWriter, r *http.Request, share v1.ShareName, id v1.UploadId, params v1.PutUploadPieceParams) {
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
	if params.Offset < 0 || r.ContentLength > maxPiece {
		probInvalidRequest.write(w)
		return
	}
	if a.cfg.MaxUpload > 0 && params.Offset+max(r.ContentLength, 0) > a.cfg.MaxUpload {
		pr := probTooLarge
		pr.limit = a.cfg.MaxUpload
		pr.write(w)
		return
	}
	conn, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()
	wd := newWatchdog(r.Context(), a.cfg.RequestTimeout)
	defer wd.stop()
	// A piece is bounded on its own as well as by the whole's limit: a body
	// without a length is cut off at maxPiece and counted as too large.
	body := &stallReader{r: http.MaxBytesReader(w, r.Body, maxPiece), rc: http.NewResponseController(w), wd: wd, stall: a.cfg.StallTimeout}
	n, err := conn.PartialAppend(wd.ctx, share, p, id, params.Offset, body, a.cfg.MaxUpload)
	a.metrics.Up(body.n)
	if err != nil {
		if smb.KindOf(err) == smb.KindLimit {
			w.Header().Set("Connection", "close")
		}
		a.fail(w, r, err)
		return
	}
	drain(r.Body)
	writeJSON(w, http.StatusOK, v1.UploadState{Size: n})
}

func (a *API) FinishUpload(w http.ResponseWriter, r *http.Request, share v1.ShareName, id v1.UploadId, params v1.FinishUploadParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolBackground)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	if params.Size < 0 {
		probInvalidRequest.write(w)
		return
	}
	e, err := conn.PartialFinish(ctx, share, p, id, params.Size, params.Overwrite != nil && *params.Overwrite)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry(parent(p), e))
}

func (a *API) DiscardUpload(w http.ResponseWriter, r *http.Request, share v1.ShareName, id v1.UploadId, params v1.DiscardUploadParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolBackground)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	if err := conn.PartialDiscard(ctx, share, p, id); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
