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

var probNotAnImage = problem{status: http.StatusUnsupportedMediaType, code: "not_an_image", title: "ferry cannot make a thumbnail of this file."}

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
		a.metrics.Thumbnail("not_modified")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if f.Size > a.cfg.ThumbnailMaxBytes {
		a.metrics.Thumbnail("too_large")
		probTooLarge.write(w)
		return
	}

	// What the thumbnail will cost is known from the header and the file's
	// size, before the file is read: the memory budget covers the source
	// buffer too, so many requests at once cannot hold many large buffers.
	head := make([]byte, min(f.Size, thumb.HeaderBytes))
	if _, err := io.ReadFull(f, head); err != nil {
		a.fail(w, r, err)
		return
	}
	cost, err := thumb.Estimate(head, f.Size, a.cfg.ThumbnailMaxPixels)
	if err == nil && cost > a.cfg.ThumbnailMemory {
		err = thumb.ErrTooLarge
	}
	if err != nil {
		a.thumbProblem(w, err)
		return
	}
	if err := a.thumbMemory.Acquire(ctx, cost); err != nil {
		probUnreachable.write(w)
		return
	}
	a.thumbUsed.Add(cost)
	defer func() {
		a.thumbUsed.Add(-cost)
		a.thumbMemory.Release(cost)
	}()

	// The rest of the image: into a buffer of the file's size, filled by the
	// file's own 1 MiB reads (io.Copy takes File.WriteTo).
	buf := bytes.NewBuffer(make([]byte, 0, f.Size))
	buf.Write(head)
	if _, err := io.Copy(buf, io.LimitReader(f, f.Size-int64(len(head)))); err != nil {
		a.fail(w, r, err)
		return
	}

	// Decoding also costs CPU: only so many run at once, whatever the memory
	// allows, and the rest wait within the request's time.
	select {
	case a.thumbs <- struct{}{}:
	case <-ctx.Done():
		probUnreachable.write(w)
		return
	}
	out, err := thumb.Make(buf.Bytes(), size, a.cfg.ThumbnailMaxPixels)
	<-a.thumbs
	if err != nil {
		a.thumbProblem(w, err)
		return
	}
	a.metrics.Thumbnail("made")
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func (a *API) thumbProblem(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, thumb.ErrUnsupported):
		a.metrics.Thumbnail("unsupported")
		probNotAnImage.write(w)
	case errors.Is(err, thumb.ErrTooLarge):
		a.metrics.Thumbnail("too_large")
		probTooLarge.write(w)
	default:
		a.metrics.Thumbnail("failed")
		a.log.Warn("thumbnail failed", errAttrs(err)...)
		probInternal.write(w)
	}
}
