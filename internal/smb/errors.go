package smb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	smb2 "github.com/cloudsoda/go-smb2"
)

// Kind is what went wrong, in the terms ferry answers in. internal/api maps
// each kind to one problem response.
type Kind int

const (
	KindInternal     Kind = iota // a bug or an answer ferry cannot interpret
	KindAuth                     // the server refused the credentials
	KindGuest                    // the server would only offer a guest or anonymous session
	KindUnreachable              // no connection to the server, or it timed out
	KindSessionLost              // the SMB connection or session is gone
	KindAccessDenied             // the server refused the operation
	KindNotFound                 // no such share, file or folder
	KindExists                   // the name is taken
	KindBusy                     // a sharing violation: someone else has it open
	KindNotEmpty                 // a folder that is not empty
	KindNotDir                   // a file where a folder was expected
	KindIsDir                    // a folder where a file was expected
	KindInvalidPath              // refused before it reached the server
	KindLimit                    // a bound of ferry's own was reached
	KindCanceled                 // the request's context ended
)

var kindNames = map[Kind]string{
	KindInternal:     "internal",
	KindAuth:         "auth",
	KindGuest:        "guest",
	KindUnreachable:  "unreachable",
	KindSessionLost:  "session_lost",
	KindAccessDenied: "access_denied",
	KindNotFound:     "not_found",
	KindExists:       "exists",
	KindBusy:         "busy",
	KindNotEmpty:     "not_empty",
	KindNotDir:       "not_dir",
	KindIsDir:        "is_dir",
	KindInvalidPath:  "invalid_path",
	KindLimit:        "limit",
	KindCanceled:     "canceled",
}

// String is the kind's name, fit for a metric label.
func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return "unknown"
}

// Error is the only error type this package returns.
//
// It deliberately carries nothing but a kind and the NT status: no message
// from go-smb2 or the network, no path, no user name. Whatever the library
// put into its errors — and a future version might put more there — stops
// here, so that nothing the user typed can reach a log line, a trace or a
// response by way of an error. It does not wrap the cause for the same
// reason.
type Error struct {
	Kind   Kind
	Status uint32 // the NTSTATUS, when the server sent one; 0 otherwise
	// Limit is the bound a KindLimit error reached — entries or bytes,
	// ferry's own configuration, never anything from the server — so that
	// the person can be told what it was. 0 when it is not one of those.
	Limit int64
}

// LimitOf is the bound a KindLimit error reached, or 0.
func LimitOf(err error) int64 {
	var e *Error
	if errors.As(err, &e) && e.Kind == KindLimit {
		return e.Limit
	}
	return 0
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("smb: %s (status 0x%08x)", e.Kind, e.Status)
	}
	return "smb: " + e.Kind.String()
}

// KindOf returns the kind of an error from this package, and KindInternal
// for anything else.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindInternal
}

// NT statuses ferry tells apart. MS-ERREF's values, repeated here because
// go-smb2 keeps its table in an internal package.
const (
	statusAccessDenied          = 0xC0000022
	statusObjectNameInvalid     = 0xC0000033
	statusObjectNameNotFound    = 0xC0000034
	statusObjectNameCollision   = 0xC0000035
	statusObjectPathNotFound    = 0xC000003A
	statusSharingViolation      = 0xC0000043
	statusDeletePending         = 0xC0000056
	statusLogonFailure          = 0xC000006D
	statusAccountRestriction    = 0xC000006E
	statusInvalidLogonHours     = 0xC000006F
	statusInvalidWorkstation    = 0xC0000070
	statusPasswordExpired       = 0xC0000071
	statusAccountDisabled       = 0xC0000072
	statusFileIsADirectory      = 0xC00000BA
	statusBadNetworkName        = 0xC00000CC
	statusDirectoryNotEmpty     = 0xC0000101
	statusNotADirectory         = 0xC0000103
	statusUserSessionDeleted    = 0xC0000203
	statusAccountLockedOut      = 0xC0000234
	statusPasswordMustChange    = 0xC0000224
	statusNetworkSessionExpired = 0xC000035C
	statusAccountExpired        = 0xC0000193
)

var statusKinds = map[uint32]Kind{
	statusAccessDenied:          KindAccessDenied,
	statusObjectNameInvalid:     KindInvalidPath,
	statusObjectNameNotFound:    KindNotFound,
	statusObjectPathNotFound:    KindNotFound,
	statusBadNetworkName:        KindNotFound,
	statusObjectNameCollision:   KindExists,
	statusSharingViolation:      KindBusy,
	statusDeletePending:         KindBusy,
	statusDirectoryNotEmpty:     KindNotEmpty,
	statusNotADirectory:         KindNotDir,
	statusFileIsADirectory:      KindIsDir,
	statusUserSessionDeleted:    KindSessionLost,
	statusNetworkSessionExpired: KindSessionLost,
	statusLogonFailure:          KindAuth,
	statusAccountRestriction:    KindAuth,
	statusInvalidLogonHours:     KindAuth,
	statusInvalidWorkstation:    KindAuth,
	statusPasswordExpired:       KindAuth,
	statusPasswordMustChange:    KindAuth,
	statusAccountDisabled:       KindAuth,
	statusAccountLockedOut:      KindAuth,
	statusAccountExpired:        KindAuth,
}

// classify turns whatever go-smb2, the network or a context returned into an
// *Error, dropping every message on the way (see Error).
func classify(err error) *Error {
	if err == nil {
		return nil
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Kind: KindCanceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: KindUnreachable}
	}
	var re *smb2.ResponseError
	if errors.As(err, &re) {
		if k, ok := statusKinds[re.Code]; ok {
			return &Error{Kind: k, Status: re.Code}
		}
		return &Error{Kind: KindInternal, Status: re.Code}
	}
	var te *smb2.TransportError
	if errors.As(err, &te) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return &Error{Kind: KindSessionLost}
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return &Error{Kind: KindUnreachable}
	}
	if errors.Is(err, os.ErrNotExist) {
		return &Error{Kind: KindNotFound}
	}
	if errors.Is(err, os.ErrExist) {
		return &Error{Kind: KindExists}
	}
	if errors.Is(err, os.ErrPermission) {
		return &Error{Kind: KindAccessDenied}
	}
	return &Error{Kind: KindInternal}
}
