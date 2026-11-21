package api

import (
	"encoding/json"
	"net/http"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/smb"
)

// problem is an RFC 9457 response: status, a stable code and a title for
// people. The title says what happened in general terms; it never carries a
// path, a user name or anything the SMB library said.
type problem struct {
	status int
	code   string
	title  string
}

func (p problem) write(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "application/problem+json")
	h.Set("Cache-Control", "no-store")
	// A problem may answer a content request, whose own headers were set
	// for the file; they do not apply to the problem.
	h.Del("Content-Disposition")
	h.Del("Content-Length")
	h.Del("Content-Range")
	h.Del("ETag")
	h.Del("Last-Modified")
	w.WriteHeader(p.status)
	_ = json.NewEncoder(w).Encode(v1.Problem{
		Type:   "about:blank",
		Title:  p.title,
		Status: p.status,
		Code:   p.code,
	})
}

var (
	probInvalidRequest    = problem{http.StatusBadRequest, "invalid_request", "The request is not valid."}
	probInvalidPath       = problem{http.StatusBadRequest, "invalid_path", "The path is not valid."}
	probMissingFerryReq   = problem{http.StatusBadRequest, "missing_ferry_request", "The request lacks the header X-Ferry-Request: 1."}
	probUnauthenticated   = problem{http.StatusUnauthorized, "unauthenticated", "Not logged in, or the session has ended."}
	probLoginFailed       = problem{http.StatusUnauthorized, "login_failed", "The server refused the login."}
	probRateLimited       = problem{http.StatusTooManyRequests, "rate_limited", "Too many login attempts; try again later."}
	probAccessDenied      = problem{http.StatusForbidden, "access_denied", "The server does not allow this."}
	probNotFound          = problem{http.StatusNotFound, "not_found", "There is no such share, file or folder."}
	probExists            = problem{http.StatusConflict, "exists", "Something of that name exists already."}
	probBusy              = problem{http.StatusConflict, "busy", "The file is in use."}
	probNotEmpty          = problem{http.StatusConflict, "not_empty", "The folder is not empty."}
	probNotAFolder        = problem{http.StatusConflict, "not_a_folder", "That is a file, not a folder."}
	probIsAFolder         = problem{http.StatusConflict, "is_a_folder", "That is a folder, not a file."}
	probTooLarge          = problem{http.StatusRequestEntityTooLarge, "too_large", "This exceeds a limit of this ferry."}
	probRangeNotSatisfied = problem{http.StatusRequestedRangeNotSatisfiable, "range_not_satisfiable", "The range lies outside the file."}
	probUnreachable       = problem{http.StatusBadGateway, "server_unreachable", "The file server cannot be reached."}
	probSessionLost       = problem{http.StatusBadGateway, "session_lost", "The connection to the file server was lost; log in again."}
	probTooManySessions   = problem{http.StatusServiceUnavailable, "too_many_sessions", "This ferry has as many sessions as it allows; try again later."}
	probInternal          = problem{http.StatusInternalServerError, "internal", "Something went wrong in ferry."}
	probIncomplete        = problem{http.StatusBadRequest, "invalid_request", "The request ended before it was complete."}
)

// smbProblem maps an error from internal/smb to its response — the one
// place where SMB statuses become HTTP statuses.
func smbProblem(err error) problem {
	switch smb.KindOf(err) {
	case smb.KindAuth, smb.KindGuest:
		return probLoginFailed
	case smb.KindUnreachable:
		return probUnreachable
	case smb.KindSessionLost:
		return probSessionLost
	case smb.KindAccessDenied:
		return probAccessDenied
	case smb.KindNotFound:
		return probNotFound
	case smb.KindExists:
		return probExists
	case smb.KindBusy:
		return probBusy
	case smb.KindNotEmpty:
		return probNotEmpty
	case smb.KindNotDir:
		return probNotAFolder
	case smb.KindIsDir:
		return probIsAFolder
	case smb.KindInvalidPath:
		return probInvalidPath
	case smb.KindLimit:
		return probTooLarge
	case smb.KindCanceled:
		return probIncomplete
	}
	return probInternal
}
