package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

// versionsTimeout bounds one listing of versions: each snapshot costs an
// open on the share, and a folder with nested datasets has many. What was
// found by then is the answer, marked truncated.
const versionsTimeout = 20 * time.Second

// ListVersions lists what the server's snapshots hold of a path
// (internal/smb, versions.go). Time and size only: who changed a file is
// not told, whatever the server knows.
func (a *API) ListVersions(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.ListVersionsParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolBackground)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, versionsTimeout)
	defer cancel()
	vs, truncated, err := conn.Versions(ctx, share, p, smb.MaxVersions)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := v1.VersionList{Versions: make([]v1.Version, 0, len(vs)), Truncated: truncated}
	for _, v := range vs {
		t := v1.EntryTypeFile
		if v.Dir {
			t = v1.EntryTypeFolder
		}
		out.Versions = append(out.Versions, v1.Version{
			At: smb.Token(v.At), Type: t, Size: v.Size, Modified: v.ModTime.UTC(), Version: version(v.Size, v.ModTime),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// atParam reads the at parameter: zero without one, a 400 for anything but
// a snapshot token exactly as smb.TokenLayout writes it.
func atParam(w http.ResponseWriter, p *v1.At) (time.Time, bool) {
	if p == nil {
		return time.Time{}, true
	}
	at, err := smb.ParseToken(*p)
	if err != nil {
		probInvalidRequest.write(w)
		return time.Time{}, false
	}
	return at, true
}

// atVariant marks what is made of a version — a cache key's variant, an
// ETag — so that it never stands for the file as it is now, nor the file
// at another snapshot.
func atVariant(s string, at time.Time) string {
	if at.IsZero() {
		return s
	}
	return s + "@" + smb.Token(at)
}

// atTag is an ETag of a version: the file's, with the snapshot in it.
func atTag(tag string, at time.Time) string {
	if at.IsZero() {
		return tag
	}
	return strings.TrimSuffix(tag, `"`) + "-" + smb.Token(at) + `"`
}

// refuseVersionWrites answers a request that would change something and
// carries at with a 400: a version is read-only, and a restore is a copy
// into the share as it is now.
func refuseVersionWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.URL.Query().Has("at") {
				probVersionReadOnly.write(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
