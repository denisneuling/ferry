package api

import (
	"archive/zip"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

// SaveArchive packs entries into a ZIP file on the share: the archive a
// download would be, written through an upload instead of to the browser —
// to a temporary name and renamed once complete, so a failure leaves
// nothing under its name. Like a copy it says how far it got as NDJSON,
// and a client that leaves stops it between two chunks.
func (a *API) SaveArchive(w http.ResponseWriter, r *http.Request, share v1.ShareName, _ v1.SaveArchiveParams) {
	var req v1.SaveArchiveRequest
	if !decodeJSON(w, r, archiveRequestBytes, &req) {
		return
	}
	paths, base, ok := a.archivePaths(w, req.Paths)
	if !ok {
		return
	}
	to, ok := cleanParam(w, &req.To)
	if !ok {
		return
	}
	if to == "" {
		probInvalidPath.write(w)
		return
	}
	// The archive would hold its own temporary file.
	for _, p := range paths {
		if to == p || strings.HasPrefix(to, p+"/") {
			probCopyIntoItself.write(w)
			return
		}
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
	conn, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		probInternal.write(w)
		return
	}
	defer given()

	pctx, pcancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	entries, err := conn.Tree(pctx, share, paths, a.cfg.MaxArchive)
	if err == nil {
		// A target that exists is refused before anything is packed; the
		// upload checks once more before its rename.
		if _, serr := conn.Stat(pctx, share, to); serr == nil {
			err = &smb.Error{Kind: smb.KindExists}
		} else if smb.KindOf(serr) != smb.KindNotFound {
			err = serr
		}
	}
	pcancel()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	total := v1.CopyProgress{TotalFiles: len(entries)}
	for _, e := range entries {
		total.TotalBytes += e.Size
	}
	if a.cfg.MaxUpload > 0 && total.TotalBytes > a.cfg.MaxUpload {
		smbProblem(&smb.Error{Kind: smb.KindLimit, Limit: a.cfg.MaxUpload}).write(w)
		return
	}

	out := &copyWriter{w: w, rc: http.NewResponseController(w), stall: a.cfg.StallTimeout, p: total, last: time.Now()}
	out.begin()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	pr, pw := io.Pipe()
	type result struct {
		e   smb.Entry
		err error
	}
	uploaded := make(chan result, 1)
	go func() {
		e, err := conn.Upload(ctx, share, to, pr, false, a.cfg.MaxUpload)
		_ = pr.CloseWithError(err) // the packing stops when the upload does
		uploaded <- result{e, err}
	}()
	err = func() error {
		zw := zip.NewWriter(pw)
		buf := make([]byte, smb.StreamChunk)
		for _, e := range entries {
			hdr := &zip.FileHeader{Name: zipName(base, e.Path), Method: zip.Store, Modified: e.ModTime}
			if e.Dir {
				hdr.Name += "/"
				if _, err := zw.CreateHeader(hdr); err != nil {
					return err
				}
				out.p.Files++
				continue
			}
			hdr.UncompressedSize64 = uint64(e.Size)
			dst, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			f, err := conn.Open(ctx, share, e.Path)
			if err != nil {
				return err
			}
			for {
				n, rerr := f.Read(buf)
				if n > 0 {
					if _, werr := dst.Write(buf[:n]); werr != nil {
						_ = f.Close()
						return werr
					}
					out.p.Bytes += int64(n)
					out.tick()
				}
				if rerr == io.EOF {
					break
				}
				if rerr != nil {
					_ = f.Close()
					return rerr
				}
			}
			_ = f.Close()
			out.p.Files++
			out.tick()
		}
		return zw.Close()
	}()
	if err != nil {
		// Ends the upload, which removes its temporary file.
		_ = pw.CloseWithError(err)
		cancel()
	} else {
		_ = pw.Close()
	}
	res := <-uploaded
	if err == nil {
		err = res.err
	} else if res.err != nil && smb.KindOf(err) == smb.KindInternal {
		err = res.err // the upload's refusal says more than the broken pipe
	}
	switch {
	case err == nil:
		_ = out.end(entry(parent(to), res.e))
	case smb.KindOf(err) == smb.KindCanceled || out.werr != nil:
	default:
		a.metrics.SMBError(smb.KindOf(err).String())
		if smb.KindOf(err) == smb.KindSessionLost {
			a.sessions.Delete(sessionID(r))
		}
		_ = out.problem(smbProblem(err))
	}
}
