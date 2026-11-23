package api

import (
	"errors"
	"net/http"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/docs"
	"github.com/denisneuling/ferry/internal/smb"
	"github.com/denisneuling/ferry/internal/zipread"
)

// maxDocumentText bounds the text a document's preview gets.
const maxDocumentText = 512 << 10

// GetDocumentText answers with the plain text of a Word or OpenDocument
// text (internal/docs): read out of its XML as the file lies on the share,
// a bounded part of it, nothing kept.
func (a *API) GetDocumentText(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetDocumentTextParams) {
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
	text, more, err := docs.Text(f, f.Size, maxDocumentText)
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
		writeJSON(w, http.StatusOK, v1.DocumentText{Text: text, Truncated: more})
	}
}
