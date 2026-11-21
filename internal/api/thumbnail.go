package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/thumb"
)

var probNotAnImage = problem{http.StatusUnsupportedMediaType, "not_an_image", "ferry cannot make a thumbnail of this file."}

func (a *API) GetThumbnail(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetThumbnailParams) {
	size := 320
	if params.Size != nil {
		size = int(*params.Size)
	}
	switch size {
	case 160, 320, 640:
	default:
		probInvalidRequest.write(w)
		return
	}
	conn, ctx, done, ok := a.onShare(w, r, share)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	f, err := conn.Open(ctx, share, p)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()

	// The ETag follows the file and the size asked for: revalidating costs
	// an open on the share, not a decode.
	tag := etag(f.Size, f.ModTime)
	tag = tag[:len(tag)-1] + "-" + strconv.Itoa(size) + `"`
	h := w.Header()
	h.Set("ETag", tag)
	h.Set("Cache-Control", "private, no-cache")
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if f.Size > a.cfg.ThumbnailMaxBytes {
		probTooLarge.write(w)
		return
	}

	// Read the whole image at once: a buffer of the file's size, filled by
	// the file's own 1 MiB reads (io.Copy takes File.WriteTo).
	var buf bytes.Buffer
	buf.Grow(int(f.Size))
	if _, err := io.Copy(&buf, io.LimitReader(f, a.cfg.ThumbnailMaxBytes+1)); err != nil {
		a.fail(w, r, err)
		return
	}
	if int64(buf.Len()) > a.cfg.ThumbnailMaxBytes {
		probTooLarge.write(w)
		return
	}

	// Decoding is the one thing here that costs memory and CPU in earnest;
	// only so many run at once, and the rest wait within the request's time.
	select {
	case a.thumbs <- struct{}{}:
	case <-ctx.Done():
		probUnreachable.write(w)
		return
	}
	out, err := thumb.Make(buf.Bytes(), size, a.cfg.ThumbnailMaxPixels)
	<-a.thumbs
	switch {
	case errors.Is(err, thumb.ErrUnsupported):
		probNotAnImage.write(w)
		return
	case errors.Is(err, thumb.ErrTooLarge):
		probTooLarge.write(w)
		return
	case err != nil:
		a.log.Warn("thumbnail failed", errAttrs(err)...)
		probInternal.write(w)
		return
	}
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}
