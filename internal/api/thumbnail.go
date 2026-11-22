package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/cache"
	"github.com/metapatrol/ferry/internal/meta"
	"github.com/metapatrol/ferry/internal/preview"
	"github.com/metapatrol/ferry/internal/smb"
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
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolBackground)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	f, again, err := a.openDerived(ctx, r, conn, share, p)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer again()
	defer func() { _ = f.Close() }()

	// The ETag follows the file and the size asked for: revalidating costs
	// an open on the share, not a decode.
	tag := etag(f.Size, f.ModTime)
	tag = tag[:len(tag)-1] + "-" + strconv.Itoa(size) + `"`
	h := w.Header()
	h.Set("ETag", tag)
	cacheDerived(h, params.V, f.Size, f.ModTime)
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, tag) {
		a.metrics.Thumbnail("not_modified")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	// The file is open for reading under this person's session: whoever
	// gets this far may have what was made of it, by whomever.
	key := derivedKey("thumbnail", strconv.Itoa(size), share, p, f)
	if a.cache != nil {
		if out, ok := a.cache.Get(key); ok {
			a.metrics.Thumbnail("cached")
			writeThumbnail(w, out)
			return
		}
	}

	// The picture is the file itself, or the cover a recording carries: then
	// only the cover is read, found by the tags' headers.
	src, srcSize := io.Reader(f), f.Size
	held := false // a cover slot, see API.coverSlots
	release := func() {
		if held {
			held = false
			<-a.coverSlots
		}
	}
	defer release()
	// 1 KB: pdfium takes a PDF whose header comes after other bytes, up to
	// 1024 of them.
	peek := make([]byte, min(f.Size, 1024))
	if _, err := f.ReadAt(peek, 0); err != nil && !errors.Is(err, io.EOF) {
		a.fail(w, r, err)
		return
	}
	if bytes.Contains(peek, []byte("%PDF-")) {
		a.pdfThumbnail(ctx, w, r, f, key, size)
		return
	}
	if isHEIC(peek) {
		a.heicThumbnail(ctx, w, r, f, key, size)
		return
	}
	if meta.IsAudio(peek) {
		at := &firstError{r: f}
		gate := func() error {
			select {
			case a.coverSlots <- struct{}{}:
				held = true
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		c, err := meta.FindCover(at, f.Size, a.cfg.ThumbnailMaxBytes, gate)
		switch {
		case at.err != nil:
			a.fail(w, r, at.err)
			return
		case ctx.Err() != nil:
			probUnreachable.write(w)
			return
		case errors.Is(err, meta.ErrCoverTooLarge):
			a.thumbProblem(w, thumb.ErrTooLarge)
			return
		case err != nil:
			a.thumbProblem(w, thumb.ErrUnsupported)
			return
		}
		src, srcSize = chunked{c.Reader()}, c.Size
	}
	if srcSize > a.cfg.ThumbnailMaxBytes {
		a.metrics.Thumbnail("too_large")
		probTooLarge.write(w)
		return
	}

	// What the thumbnail will cost is known from the header and the file's
	// size, before the file is read: the memory budget covers the source
	// buffer too, so many requests at once cannot hold many large buffers.
	head := make([]byte, min(srcSize, thumb.HeaderBytes))
	if _, err := io.ReadFull(src, head); err != nil {
		a.fail(w, r, err)
		return
	}
	cost, err := thumb.Estimate(head, srcSize, a.cfg.ThumbnailMaxPixels)
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
	release() // the cover, if one was held in memory, is counted now
	a.thumbUsed.Add(cost)
	defer func() {
		a.thumbUsed.Add(-cost)
		a.thumbMemory.Release(cost)
	}()

	// The rest of the image: into a buffer of its size, filled by the file's
	// own 1 MiB reads (io.Copy takes File.WriteTo).
	buf := bytes.NewBuffer(make([]byte, 0, srcSize))
	buf.Write(head)
	if _, err := io.Copy(buf, io.LimitReader(src, srcSize-int64(len(head)))); err != nil {
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
	if a.cache != nil {
		a.cache.Put(key, out)
	}
	writeThumbnail(w, out)
}

// pdfThumbnail renders a PDF's first page through the preview engine,
// which charges the thumbnail budget itself. The engine reads the file as
// pdfium asks — the cross-reference table, the first page's objects —
// rather than all of it, so FERRY_THUMBNAIL_MAX_BYTES does not apply.
func (a *API) pdfThumbnail(ctx context.Context, w http.ResponseWriter, r *http.Request, f *smb.File, key cache.Key, size int) {
	src := &firstErrorSeeker{f: f}
	out, err := a.preview.PDF(ctx, src, f.Size, size)
	switch {
	case src.err != nil:
		// The share failed under pdfium; what pdfium made of that is beside
		// the point.
		a.fail(w, r, src.err)
	case err == nil:
		a.metrics.Thumbnail("made")
		if a.cache != nil {
			a.cache.Put(key, out)
		}
		writeThumbnail(w, out)
	case ctx.Err() != nil:
		probUnreachable.write(w)
	case errors.Is(err, preview.ErrUnsupported), errors.Is(err, preview.ErrBlank), errors.Is(err, preview.ErrUnavailable):
		// Off, unreadable, or white — perhaps for want of memory: an icon,
		// and nothing cached.
		a.thumbProblem(w, thumb.ErrUnsupported)
	default:
		a.thumbProblem(w, err)
	}
}

// heicThumbnail makes a HEIC photo's thumbnail through the preview engine:
// up to 320 from the thumbnail the photo carries, larger from the photo,
// within FERRY_THUMBNAIL_MAX_BYTES and FERRY_THUMBNAIL_MAX_PIXELS.
func (a *API) heicThumbnail(ctx context.Context, w http.ResponseWriter, r *http.Request, f *smb.File, key cache.Key, size int) {
	src := &firstError{r: f}
	out, err := a.preview.HEIC(ctx, src, f.Size, size, a.cfg.ThumbnailMaxBytes, a.cfg.ThumbnailMaxPixels)
	switch {
	case src.err != nil:
		a.fail(w, r, src.err)
	case err == nil:
		a.metrics.Thumbnail("made")
		if a.cache != nil {
			a.cache.Put(key, out)
		}
		writeThumbnail(w, out)
	case ctx.Err() != nil:
		probUnreachable.write(w)
	case errors.Is(err, preview.ErrTooLarge):
		a.thumbProblem(w, thumb.ErrTooLarge)
	default:
		// Off, unreadable, or more than the instance may hold: an icon.
		a.thumbProblem(w, thumb.ErrUnsupported)
	}
}

// isHEIC tells a HEIF photo coded in HEVC by its ftyp brands; AVIF and
// other HEIF flavours libheif is not built for are left alone.
func isHEIC(h []byte) bool {
	if len(h) < 12 || string(h[4:8]) != "ftyp" {
		return false
	}
	n := int(h[0])<<24 | int(h[1])<<16 | int(h[2])<<8 | int(h[3])
	n = min(n, len(h))
	for i := 8; i+4 <= n; i += 4 {
		if i == 12 {
			continue // the minor version
		}
		switch string(h[i : i+4]) {
		case "heic", "heix", "heim", "heis", "hevc", "hevx":
			return true
		}
	}
	return false
}

// firstErrorSeeker is a file read by pdfium: its first failure of the
// share is kept, as pdfium reports any failed read as a damaged file.
type firstErrorSeeker struct {
	f   *smb.File
	err error
}

func (s *firstErrorSeeker) Read(p []byte) (int, error) {
	n, err := s.f.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && s.err == nil {
		s.err = err
	}
	return n, err
}

func (s *firstErrorSeeker) Seek(off int64, whence int) (int64, error) {
	n, err := s.f.Seek(off, whence)
	if err != nil && s.err == nil {
		s.err = err
	}
	return n, err
}

func writeThumbnail(w http.ResponseWriter, out []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
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

// firstError keeps the first error the file gave a parser that, on a
// malformed file, carries on without one: a connection that failed while
// looking for a cover is a failure, not a recording without one.
type firstError struct {
	r   io.ReaderAt
	err error
}

func (e *firstError) ReadAt(p []byte, off int64) (int, error) {
	n, err := e.r.ReadAt(p, off)
	if err != nil && !errors.Is(err, io.EOF) && e.err == nil {
		e.err = err
	}
	return n, err
}

// chunked reads at most smb.StreamChunk at a time: a cover is a part of the
// file, read with ReadAt, and without it one request would read the whole
// picture in a single call (docs/spike.md, the 1 MiB reads).
type chunked struct{ r io.Reader }

func (c chunked) Read(p []byte) (int, error) {
	if len(p) > smb.StreamChunk {
		p = p[:smb.StreamChunk]
	}
	return c.r.Read(p)
}
