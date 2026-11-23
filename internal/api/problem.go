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
	limit  int64 // for too_large: the bound reached, when it is worth telling
}

// body is the problem as the API writes it.
func (p problem) body() v1.Problem {
	b := v1.Problem{Type: "about:blank", Title: p.title, Status: p.status, Code: p.code}
	if p.limit > 0 {
		b.Limit = &p.limit
	}
	return b
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
	_ = json.NewEncoder(w).Encode(p.body())
}

var (
	probInvalidRequest    = problem{status: http.StatusBadRequest, code: "invalid_request", title: "The request is not valid."}
	probInvalidPath       = problem{status: http.StatusBadRequest, code: "invalid_path", title: "The path is not valid."}
	probMissingFerryReq   = problem{status: http.StatusBadRequest, code: "missing_ferry_request", title: "The request lacks the header X-Ferry-Request: 1."}
	probUnauthenticated   = problem{status: http.StatusUnauthorized, code: "unauthenticated", title: "Not logged in, or the session has ended."}
	probLoginFailed       = problem{status: http.StatusUnauthorized, code: "login_failed", title: "The server refused the login."}
	probRateLimited       = problem{status: http.StatusTooManyRequests, code: "rate_limited", title: "Too many login attempts; try again later."}
	probAccessDenied      = problem{status: http.StatusForbidden, code: "access_denied", title: "The server does not allow this."}
	probNotFound          = problem{status: http.StatusNotFound, code: "not_found", title: "There is no such share, file or folder."}
	probExists            = problem{status: http.StatusConflict, code: "exists", title: "Something of that name exists already."}
	probBusy              = problem{status: http.StatusConflict, code: "busy", title: "The file is in use."}
	probNotEmpty          = problem{status: http.StatusConflict, code: "not_empty", title: "The folder is not empty."}
	probNotAFolder        = problem{status: http.StatusConflict, code: "not_a_folder", title: "That is a file, not a folder."}
	probIsAFolder         = problem{status: http.StatusConflict, code: "is_a_folder", title: "That is a folder, not a file."}
	probTooLarge          = problem{status: http.StatusRequestEntityTooLarge, code: "too_large", title: "This exceeds a limit of this ferry."}
	probRangeNotSatisfied = problem{status: http.StatusRequestedRangeNotSatisfiable, code: "range_not_satisfiable", title: "The range lies outside the file."}
	probUnreachable       = problem{status: http.StatusBadGateway, code: "server_unreachable", title: "The file server cannot be reached."}
	probSessionLost       = problem{status: http.StatusBadGateway, code: "session_lost", title: "The connection to the file server was lost; log in again."}
	probPreviewTimeout    = problem{status: http.StatusServiceUnavailable, code: "preview_timeout", title: "Making this preview took too long; try again later."}
	probTooManySessions   = problem{status: http.StatusServiceUnavailable, code: "too_many_sessions", title: "This ferry has as many sessions as it allows; try again later."}
	probInternal          = problem{status: http.StatusInternalServerError, code: "internal", title: "Something went wrong in ferry."}
	probIncomplete        = problem{status: http.StatusBadRequest, code: "invalid_request", title: "The request ended before it was complete."}
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
		p := probTooLarge
		p.limit = smb.LimitOf(err)
		return p
	case smb.KindCanceled:
		return probIncomplete
	}
	return probInternal
}
