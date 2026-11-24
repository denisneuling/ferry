package api

import (
	"errors"
	"net/http"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/docs"
	"github.com/denisneuling/ferry/internal/smb"
	"github.com/denisneuling/ferry/internal/zipread"
)

// GetSheet answers with the first sheet of a workbook (internal/docs): its
// cells as text, read out of its XML as the file lies on the share, a
// bounded part of it, nothing kept.
func (a *API) GetSheet(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetSheetParams) {
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
	index := 0
	if params.Sheet != nil {
		index = *params.Sheet
	}
	s, err := docs.ReadSheet(f, f.Size, docs.DefaultSheetLimits, index)
	switch {
	case errors.Is(err, zipread.ErrTooLarge):
		probTooLarge.write(w)
	case isDocumentErr(err):
		probNotADocument.write(w)
	case err != nil && !errors.As(err, new(*smb.Error)):
		// The XML itself is broken: still a file, not a failure of ferry's.
		probNotADocument.write(w)
	case err != nil:
		a.fail(w, r, err)
	default:
		if s.Rows == nil {
			s.Rows = [][]string{}
		}
		if s.Names == nil {
			s.Names = []string{}
		}
		writeJSON(w, http.StatusOK, v1.Sheet{Name: s.Name, Names: s.Names, Index: s.Index, Rows: s.Rows, Truncated: s.Truncated})
	}
}
