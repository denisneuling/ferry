package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
	"github.com/denisneuling/ferry/internal/unpack"
	"github.com/denisneuling/ferry/internal/zipread"
)

// maxUnpackDepth bounds how many folders deep an archive unpacks.
const maxUnpackDepth = 32

var (
	probArchiveUnsafe    = problem{status: http.StatusUnprocessableEntity, code: "archive_unsafe_name", title: "An entry of the archive has a name that would leave its folder."}
	probArchiveForbidden = problem{status: http.StatusUnprocessableEntity, code: "archive_forbidden_name", title: "An entry of the archive has a character in its name that the server does not take."}
	probArchiveDeep      = problem{status: http.StatusUnprocessableEntity, code: "archive_too_deep", title: "The archive's folders go deeper than ferry unpacks."}
	probArchiveCollision = problem{status: http.StatusConflict, code: "archive_collision", title: "Two entries of the archive would get the same name on the share."}
	probNoSpace          = problem{status: http.StatusInsufficientStorage, code: "no_space", title: "There is not enough free space for what the archive holds."}
)

// UnpackArchive unpacks a ZIP archive into a new folder (internal/unpack
// decides what it writes). The archive is read through the person's
// session, as it lies on the share, and every file written as an upload
// is: to a temporary name, renamed once complete. Everything that could be
// refused is refused before the first write; then it runs like a copy,
// with its progress as NDJSON, not bounded by the request timeout, and a
// client that leaves stops it between two chunks.
func (a *API) UnpackArchive(w http.ResponseWriter, r *http.Request, share v1.ShareName, _ v1.UnpackArchiveParams) {
	var req v1.UnpackRequest
	if !decodeJSON(w, r, 16<<10, &req) {
		return
	}
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	if !a.cfg.AllowsShare(share) {
		probNotFound.write(w)
		return
	}
	from, ok := cleanParam(w, &req.From)
	if !ok {
		return
	}
	to, ok := cleanParam(w, &req.To)
	if !ok {
		return
	}
	at, ok := atParam(w, req.At)
	if !ok {
		return
	}
	if from == "" || to == "" {
		probInvalidPath.write(w)
		return
	}
	conn, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()

	// The archive's table and the checks, under the request timeout.
	pctx, pcancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	defer pcancel()
	// The archive is read until the end of the request, not only of the
	// checks: its reads check the context it was opened with.
	f, err := openFile(r.Context(), conn, share, from, at)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()
	es, err := zipread.List(f, f.Size, a.cfg.MaxArchive)
	if a.zipFailed(w, r, err) {
		return
	}
	plan, err := unpack.Make(es, unpack.Limits{Entries: a.cfg.MaxArchive, Depth: maxUnpackDepth, Bytes: a.cfg.MaxUpload})
	switch {
	case errors.Is(err, unpack.ErrUnsafeName):
		probArchiveUnsafe.write(w)
		return
	case errors.Is(err, unpack.ErrForbiddenName):
		probArchiveForbidden.write(w)
		return
	case errors.Is(err, unpack.ErrTooDeep):
		probArchiveDeep.write(w)
		return
	case errors.Is(err, unpack.ErrCollision):
		probArchiveCollision.write(w)
		return
	case errors.Is(err, unpack.ErrUnreadable):
		probNotAnArchive.write(w)
		return
	case errors.Is(err, unpack.ErrTooMany), errors.Is(err, zipread.ErrTooLarge):
		probTooLarge.write(w)
		return
	case err != nil:
		probInternal.write(w)
		return
	}
	// The server's word on free space, where it gives one: what it reports
	// is what this person may still write, a quota included.
	if sp, serr := conn.Space(pctx, share, parent(to)); serr == nil && sp.Free > 0 && uint64(plan.Bytes) > sp.Free {
		probNoSpace.write(w)
		return
	}
	made, err := conn.Mkdir(pctx, share, to)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	pcancel()

	total := v1.CopyProgress{TotalFiles: 1 + len(plan.Folders) + len(plan.Files), TotalBytes: plan.Bytes, Files: 1}
	out := &copyWriter{w: w, rc: http.NewResponseController(w), stall: a.cfg.StallTimeout, p: total, last: time.Now()}
	out.begin()
	ctx := r.Context()
	blocks := &blockReader{r: f, size: f.Size}
	for _, d := range plan.Folders {
		if _, err = conn.Mkdir(ctx, share, to+"/"+d); err != nil {
			break
		}
		out.p.Files++
		out.tick()
	}
	broken := false // the archive's data, not the share, failed
	for _, it := range plan.Files {
		if err != nil {
			break
		}
		var rd io.Reader
		if rd, err = zipread.Open(blocks, f.Size, it.Entry, it.Entry.Size); err != nil {
			// The table said it could be read; its data says otherwise.
			err = &smb.Error{Kind: smb.KindInternal}
			break
		}
		counted := &countReader{r: rd, n: func(n int) { out.p.Bytes += int64(n); out.tick() }}
		if _, err = conn.Upload(ctx, share, to+"/"+it.Path, counted, false, a.cfg.MaxUpload); err != nil {
			if counted.err != nil {
				broken = true
			}
			break
		}
		out.p.Files++
		out.tick()
	}
	switch {
	case err == nil:
		_ = out.end(entry(parent(to), made))
	case broken:
		_ = out.problem(probNotAnArchive)
	case smb.KindOf(err) == smb.KindCanceled || out.werr != nil:
	default:
		a.metrics.SMBError(smb.KindOf(err).String())
		if smb.KindOf(err) == smb.KindSessionLost {
			a.sessions.Delete(sessionID(r))
		}
		_ = out.problem(smbProblem(err))
	}
}

// countReader tells how many bytes went through, and keeps the archive's
// own read error apart from the share's.
type countReader struct {
	r   io.Reader
	n   func(int)
	err error
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.n(n)
	}
	if err != nil && err != io.EOF {
		c.err = err
	}
	return n, err
}

// blockReader reads the archive a block at a time and serves the small reads
// a deflate stream makes from it: each read on the share is a round trip,
// and 1 MiB ones are what makes reading fast (docs/spike.md).
type blockReader struct {
	r     io.ReaderAt
	size  int64
	block []byte
	at    int64
}

func (b *blockReader) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	for n < len(p) {
		o := off + int64(n)
		if o >= b.size {
			return n, io.EOF
		}
		if b.block == nil || o < b.at || o >= b.at+int64(len(b.block)) {
			start := o - o%smb.StreamChunk
			buf := make([]byte, min(int64(smb.StreamChunk), b.size-start))
			got, err := b.r.ReadAt(buf, start)
			if got < len(buf) {
				if err == nil {
					err = io.ErrUnexpectedEOF
				}
				return n, err
			}
			b.block, b.at = buf, start
		}
		n += copy(p[n:], b.block[o-b.at:])
	}
	return n, nil
}
