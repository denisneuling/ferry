package api

import (
	"errors"
	"net/http"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/epub"
	"github.com/denisneuling/ferry/internal/smb"
	"github.com/denisneuling/ferry/internal/zipread"
)

var probBookEncrypted = problem{status: http.StatusUnsupportedMediaType, code: "book_encrypted", title: "This book is encrypted (DRM) and cannot be read here."}

// GetBook answers with an EPUB book's title and chapters (internal/epub).
func (a *API) GetBook(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetBookParams) {
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
	b, _, err := epub.Open(f, f.Size)
	if a.bookFailed(w, r, err) {
		return
	}
	out := v1.Book{Title: b.Title, Chapters: make([]struct {
		Entry string `json:"entry"`
		Title string `json:"title"`
	}, 0, len(b.Chapters))}
	for _, c := range b.Chapters {
		out.Chapters = append(out.Chapters, struct {
			Entry string `json:"entry"`
			Title string `json:"title"`
		}{c.Entry, c.Title})
	}
	writeJSON(w, http.StatusOK, out)
}

// GetBookChapter answers with one chapter of a book as a tree of text:
// nothing of its XHTML is passed on as markup (internal/epub).
func (a *API) GetBookChapter(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetBookChapterParams) {
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
	b, es, err := epub.Open(f, f.Size)
	if a.bookFailed(w, r, err) {
		return
	}
	nodes, more, err := epub.ReadChapter(f, f.Size, es, b, params.Entry)
	if a.bookFailed(w, r, err) {
		return
	}
	if nodes == nil {
		nodes = []epub.Node{}
	}
	writeJSON(w, http.StatusOK, struct {
		Nodes     []epub.Node `json:"nodes"`
		Truncated bool        `json:"truncated"`
	}{nodes, more})
}

// bookFailed answers for an error reading a book: about the file, or the
// share's.
func (a *API) bookFailed(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, epub.ErrEncrypted):
		probBookEncrypted.write(w)
	case errors.Is(err, zipread.ErrTooLarge):
		probTooLarge.write(w)
	case errors.Is(err, epub.ErrNotBook), isDocumentErr(err):
		probNotADocument.write(w)
	case !errors.As(err, new(*smb.Error)):
		probNotADocument.write(w)
	default:
		a.fail(w, r, err)
	}
	return true
}
