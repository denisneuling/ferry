// Package api is ferry's HTTP API: it implements api/v1's ServerInterface on
// top of the session table and internal/smb, maps SMB errors to problem
// responses (problem.go) and streams files both ways (content.go). It holds
// no rules about access: those are the SMB server's.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"sync/atomic"

	"golang.org/x/sync/semaphore"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/cache"
	"github.com/denisneuling/ferry/internal/config"
	"github.com/denisneuling/ferry/internal/metrics"
	"github.com/denisneuling/ferry/internal/preview"
	"github.com/denisneuling/ferry/internal/session"
	"github.com/denisneuling/ferry/internal/smb"
)

// LoginFunc sets up a person's SMB sessions; smb.LoginGroup, or a stand-in
// in tests.
type LoginFunc func(context.Context, smb.LoginOptions, smb.Credentials) (*smb.Group, error)

// API serves /api/v1.
type API struct {
	cfg       config.Config
	sessions  *session.Table
	byAddress *session.Limiter
	byUser    *session.Limiter
	login     LoginFunc
	log       *slog.Logger
	version   string
	// thumbs admits FERRY_THUMBNAIL_CONCURRENCY thumbnail decodes at once;
	// thumbMemory holds them within FERRY_THUMBNAIL_MEMORY together, and
	// thumbUsed is what it holds now, for the metrics.
	thumbs chan struct{}
	// coverSlots admits as many covers into memory before the memory budget
	// counts them (meta.FindCover's gate): an Ogg comment's base64, an ID3
	// tag's unsynchronisation. A request holds its slot until its thumbnail
	// has taken its share of the budget, so the uncounted memory is bounded
	// by the slots, about 24 MiB each (operations.md).
	coverSlots chan struct{}
	// metaSlots admits metaConcurrency metadata readings at once.
	metaSlots   chan struct{}
	thumbMemory *semaphore.Weighted
	// failures are files that made no thumbnail a moment ago (failures.go).
	failures  *failures
	thumbUsed atomic.Int64
	metrics   *metrics.Metrics
	// archives holds the ZIP downloads listed and not yet fetched.
	archives archiveTickets
	// cache keeps thumbnails and details across restarts and people; nil
	// without FERRY_CACHE_DIR.
	cache *cache.Cache
	// preview makes thumbnails of PDFs, as WebAssembly, charged to
	// thumbMemory like any thumbnail.
	preview *preview.Engine
}

// budget takes n bytes from the thumbnail budget for the previews; more
// than the whole budget fails at once instead of waiting for ever.
func (a *API) budget(ctx context.Context, n int64) (func(), error) {
	if n > a.cfg.ThumbnailMemory {
		return nil, fmt.Errorf("%d bytes is more than FERRY_THUMBNAIL_MEMORY", n)
	}
	if err := a.thumbMemory.Acquire(ctx, n); err != nil {
		return nil, err
	}
	a.thumbUsed.Add(n)
	return func() {
		a.thumbUsed.Add(-n)
		a.thumbMemory.Release(n)
	}, nil
}

// Close lets go of what the API holds beyond requests: the previews'
// runtimes.
func (a *API) Close() { a.preview.Close() }

// UseCache makes the API keep thumbnails and details in c. An entry is
// looked up only after the person's own session has opened the file for
// reading, so that Samba decides who gets it.
func (a *API) UseCache(c *cache.Cache) { a.cache = c }

// derivedKey is what a thumbnail or a file's details is kept under: the file
// as the open just found it.
func derivedKey(kind, variant, share, p string, f *smb.File) cache.Key {
	return cache.Key{Kind: kind, Variant: variant, Share: share, Path: p, Size: f.Size, ModTime: f.ModTime}
}

// New makes the API. login is smb.LoginGroup outside tests; m may be nil.
func New(cfg config.Config, sessions *session.Table, login LoginFunc, log *slog.Logger, version string, m *metrics.Metrics) *API {
	a := &API{
		metrics:     m,
		cfg:         cfg,
		sessions:    sessions,
		byAddress:   session.NewLimiter(cfg.LoginRatePerAddress, 10000, nil),
		byUser:      session.NewLimiter(cfg.LoginRatePerUser, 10000, nil),
		login:       login,
		log:         log,
		version:     version,
		thumbs:      make(chan struct{}, max(1, cfg.ThumbnailConcurrency)),
		coverSlots:  make(chan struct{}, max(1, cfg.ThumbnailConcurrency)),
		metaSlots:   make(chan struct{}, metaConcurrency),
		thumbMemory: semaphore.NewWeighted(max(1, cfg.ThumbnailMemory)),
		failures:    newFailures(),
	}
	o := preview.Options{
		PDF: cfg.PreviewPDF, CodeDir: cfg.WASMDir, Budget: a.budget, BudgetSize: cfg.ThumbnailMemory,
		Concurrency: cfg.PreviewConcurrency, Timeout: cfg.PreviewTimeout, Observe: m.Preview, Log: log,
		HEIC: cfg.PreviewHEIC, HEIFMemory: cfg.PreviewHEICMemory,
	}
	if cfg.PreviewHEIC && cfg.HEIFWASM != "" {
		// The operator's module (third_party/heif/README.md); one that cannot
		// be read leaves HEIC previews off rather than ferry down.
		if b, err := os.ReadFile(cfg.HEIFWASM); err != nil {
			log.Warn("heic previews are off: FERRY_HEIF_WASM cannot be read", "error", err.Error())
			o.HEIC = false
		} else {
			o.HEIFModule = b
		}
	}
	if (cfg.PreviewPDF || cfg.PreviewHEIC) && cfg.RequestTimeout <= cfg.PreviewTimeout {
		// The request ends first: a slow job is cut off as a request that
		// ran out, never as one that took too long.
		log.Warn("FERRY_PREVIEW_TIMEOUT does not take effect: FERRY_REQUEST_TIMEOUT ends a thumbnail's request before",
			"preview_timeout", cfg.PreviewTimeout.String(), "request_timeout", cfg.RequestTimeout.String())
	}
	a.preview = preview.New(o)
	return a
}

var _ v1.ServerInterface = (*API)(nil)

// Handler returns the API's routes with ferry's checks in front of them: the
// CSRF header on every request that changes something, and the headers
// every API answer carries. They wrap the generated router rather than run
// as its middlewares, because the router binds parameters first — a missing
// X-Ferry-Request would otherwise be a generic parameter error.
func (a *API) Handler() http.Handler {
	h := v1.HandlerWithOptions(a, v1.StdHTTPServerOptions{
		BaseRouter: http.NewServeMux(),
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			// Parameter errors from the generated code: which parameter,
			// never its value.
			probInvalidRequest.write(w)
		},
	})
	return apiHeaders(requireFerryRequest(refuseVersionWrites(h)))
}

// requireFerryRequest refuses a request that changes something unless it
// carries X-Ferry-Request: 1. A cross-site form or a simple fetch cannot set
// a custom header, and one that tries needs a CORS preflight ferry never
// answers; with the SameSite=Strict cookie this is the CSRF protection.
func requireFerryRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Ferry-Request") != "1" {
				probMissingFerryReq.write(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// apiHeaders are what every API answer carries. A file download replaces
// the Content-Security-Policy with its own sandbox (content.go).
func apiHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON reads a small JSON body into v, refusing unknown fields and
// anything beyond max bytes.
func decodeJSON(w http.ResponseWriter, r *http.Request, max int64, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, max))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			probTooLarge.write(w)
		} else {
			probInvalidRequest.write(w)
		}
		return false
	}
	if dec.More() {
		probInvalidRequest.write(w)
		return false
	}
	return true
}

// ── Cookies and client addresses ───────────────────────────────────────────

const (
	cookieName       = "ferry_session"
	secureCookieName = "__Host-ferry_session"
)

// secure reports whether the session cookie should be Secure for this
// request. "auto" believes X-Forwarded-Proto from anyone: a forged header
// can only make the cookie stricter.
func (a *API) secure(r *http.Request) bool {
	switch a.cfg.CookieSecure {
	case "true":
		return true
	case "false":
		return false
	}
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *API) setCookie(w http.ResponseWriter, r *http.Request, id string) {
	c := &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	if a.secure(r) {
		c.Name, c.Secure = secureCookieName, true
	}
	http.SetCookie(w, c)
}

func (a *API) clearCookie(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{cookieName, secureCookieName} {
		c := &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode}
		if name == secureCookieName {
			if !a.secure(r) {
				continue
			}
			c.Secure = true
		}
		http.SetCookie(w, c)
	}
}

func sessionID(r *http.Request) string {
	for _, name := range []string{secureCookieName, cookieName} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" && len(c.Value) <= 64 {
			return c.Value
		}
	}
	return ""
}

// clientAddr is the address login attempts are counted against: the TCP
// peer, or — when the peer is a trusted proxy — the last address in
// X-Forwarded-For that is not one.
func (a *API) clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !a.trusted(peer) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !a.trusted(hop) {
			return hop.String()
		}
	}
	return host
}

func (a *API) trusted(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range a.cfg.TrustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ── Session and login ──────────────────────────────────────────────────────

func (a *API) GetInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, v1.Info{
		Server:                 a.cfg.ServerName,
		IdleTimeoutSeconds:     int(a.cfg.IdleTimeout / time.Second),
		SessionLifetimeSeconds: int(a.cfg.SessionLifetime / time.Second),
		Version:                a.version,
		Limits: v1.Limits{
			Listing: a.cfg.MaxListing,
			Delete:  a.cfg.MaxDelete,
			Upload:  a.cfg.MaxUpload,
			Archive: a.cfg.MaxArchive,
		},
		Previews: previews(a.preview.Formats()),
	})
}

func previews(formats []string) *[]v1.InfoPreviews {
	if len(formats) == 0 {
		return nil
	}
	out := make([]v1.InfoPreviews, len(formats))
	for i, f := range formats {
		out[i] = v1.InfoPreviews(f)
	}
	return &out
}

const (
	maxUsername = 256
	maxPassword = 1024
)

func (a *API) Login(w http.ResponseWriter, r *http.Request, _ v1.LoginParams) {
	var req v1.LoginRequest
	if !decodeJSON(w, r, 4<<10, &req) {
		return
	}
	if req.Username == "" || req.Password == "" || len(req.Username) > maxUsername || len(req.Password) > maxPassword {
		a.metrics.Login("invalid")
		probInvalidRequest.write(w)
		return
	}
	addr := a.clientAddr(r)
	if ok, wait := a.byAddress.Allow("a:" + addr); !ok {
		a.rateLimited(w, wait, "address")
		return
	}
	if ok, wait := a.byUser.Allow("u:" + strings.ToLower(req.Username)); !ok {
		a.rateLimited(w, wait, "username")
		return
	}

	user, domain := splitUser(req.Username)
	start := time.Now()
	opts := smb.LoginOptions{Server: a.cfg.SMBServer, Timeout: a.cfg.LoginTimeout, CallTimeout: a.cfg.RequestTimeout,
		Connections: a.cfg.SMBConnections}
	if a.metrics != nil {
		opts.Observer = a.metrics
	}
	// Every connection is set up here, while the password exists: none can
	// be added later.
	conn, err := a.login(r.Context(), opts, smb.Credentials{User: user, Password: req.Password, Domain: domain})
	// The password is not needed again. Go cannot wipe a string, but nothing
	// below holds a reference to it.
	req.Password = ""
	if err != nil {
		// The kind and the NT status only: never the username, which a
		// person may have mistyped their password into.
		a.log.Info("login refused", append(errAttrs(err), "outcome", smb.KindOf(err).String(), "client", addr,
			"duration_ms", time.Since(start).Milliseconds())...)
		a.metrics.Login(smb.KindOf(err).String())
		smbProblem(err).write(w)
		return
	}
	// A login replaces the session the browser had, if any.
	if old := sessionID(r); old != "" {
		a.sessions.Delete(old)
	}
	id, s, err := a.sessions.Create(conn, req.Username)
	if err != nil {
		_ = conn.Close()
		if errors.Is(err, session.ErrFull) {
			a.log.Warn("login refused", "outcome", "too_many_sessions", "client", addr)
			a.metrics.Login("too_many_sessions")
			probTooManySessions.write(w)
			return
		}
		a.log.Error("login failed", append(errAttrs(err), "outcome", "internal")...)
		a.metrics.Login("internal")
		probInternal.write(w)
		return
	}
	a.log.Info("login", "outcome", "ok", "client", addr, "duration_ms", time.Since(start).Milliseconds())
	a.metrics.Login("ok")
	a.setCookie(w, r, id)
	writeJSON(w, http.StatusOK, a.me(s))
}

// splitUser takes the domain off DOMAIN\user. A plain name and user@domain
// go to the server as they are.
func splitUser(name string) (user, domain string) {
	if i := strings.IndexByte(name, '\\'); i > 0 {
		return name[i+1:], name[:i]
	}
	return name, ""
}

func (a *API) rateLimited(w http.ResponseWriter, wait time.Duration, by string) {
	secs := int(wait/time.Second) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	a.log.Info("login refused", "outcome", "rate_limited", "by", by)
	a.metrics.Login("rate_limited")
	probRateLimited.write(w)
}

func (a *API) Logout(w http.ResponseWriter, r *http.Request, _ v1.LogoutParams) {
	if id := sessionID(r); id != "" {
		a.sessions.Delete(id)
	}
	a.clearCookie(w, r)
	// Thumbnails and details are cached for a year (cacheDerived); they
	// leave with the session, so that the next person at a shared browser
	// cannot page back to them. Browsers act on it only in a secure context
	// (HTTPS, or localhost).
	w.Header().Set("Clear-Site-Data", `"cache"`)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) GetMe(w http.ResponseWriter, r *http.Request) {
	s, ok := a.acquire(w, r)
	if !ok {
		return
	}
	defer a.sessions.Release(s)
	writeJSON(w, http.StatusOK, a.me(s))
}

func (a *API) me(s *session.Session) v1.Me {
	return v1.Me{
		Username:           s.User,
		Server:             a.cfg.ServerName,
		ExpiresAt:          s.Expires.UTC(),
		IdleTimeoutSeconds: int(a.cfg.IdleTimeout / time.Second),
	}
}

// acquire finds the request's session, or answers 401. The caller releases
// it.
func (a *API) acquire(w http.ResponseWriter, r *http.Request) (*session.Session, bool) {
	s, ok := a.sessions.Acquire(sessionID(r))
	if !ok {
		a.clearCookie(w, r)
		probUnauthenticated.write(w)
		return nil, false
	}
	return s, true
}

// fail answers an error from internal/smb. A lost interactive connection
// ends the ferry session: there is no password to reconnect with. A lost
// background connection does not: the group carries on with the others
// (smb.Group), and this one request fails as if the server could not be
// reached.
func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	p := smbProblem(err)
	if smb.KindOf(err) == smb.KindSessionLost {
		// The pool and the route only: who it was, and what they opened,
		// stay out of the log.
		if a.groupLost(r) {
			a.log.Warn("smb connection lost", "pool", smb.PoolInteractive, "route", r.Pattern, "session", "ended")
			a.sessions.Delete(sessionID(r))
			a.clearCookie(w, r)
		} else {
			a.log.Warn("smb connection lost", "pool", smb.PoolBackground, "route", r.Pattern, "session", "continues")
			p = probUnreachable
		}
	}
	a.metrics.SMBError(smb.KindOf(err).String())
	if p.status >= 500 {
		a.log.Warn("request failed", append(errAttrs(err), "route", r.Pattern)...)
	}
	p.write(w)
}

// groupLost reports whether the request's session is over: gone from the
// table, or its interactive connection lost.
func (a *API) groupLost(r *http.Request) bool {
	s, ok := a.sessions.Acquire(sessionID(r))
	if !ok {
		return true
	}
	defer a.sessions.Release(s)
	return s.Conn.Lost()
}

// openDerived opens a file for what ferry derives from it — a thumbnail,
// its details — on c, a background connection. Should that connection have
// gone away, it tries once more on another of the group: a small read the
// person did not start, which is worth not failing for a dropped
// connection. Content and uploads do not: what a person started fails as
// it is. done gives the second connection back.
func (a *API) openDerived(ctx context.Context, r *http.Request, c *smb.Session, share, p string, at time.Time) (_ *smb.File, done func(), err error) {
	f, err := openFile(ctx, c, share, p, at)
	if smb.KindOf(err) != smb.KindSessionLost || a.groupLost(r) {
		return f, func() {}, err
	}
	s, ok := a.sessions.Acquire(sessionID(r))
	if !ok {
		return nil, func() {}, err
	}
	defer a.sessions.Release(s)
	other, given, ok := connFor(s, smb.PoolBackground)
	if !ok {
		return nil, func() {}, err
	}
	a.log.Warn("smb connection lost", "pool", smb.PoolBackground, "route", r.Pattern, "session", "continues", "retry", true)
	f, err = openFile(ctx, other, share, p, at)
	if err != nil {
		given()
		return nil, func() {}, err
	}
	return f, given, nil
}

// openFile opens a file as it is now, or as the snapshot at at holds it.
func openFile(ctx context.Context, c *smb.Session, share, p string, at time.Time) (*smb.File, error) {
	if at.IsZero() {
		return c.Open(ctx, share, p)
	}
	return c.OpenAt(ctx, share, p, at)
}

// connFor is the SMB session of a person's group that serves the pool: the
// interactive connection, or one of the background ones. done gives a
// background one back.
func connFor(s *session.Session, pool string) (_ *smb.Session, done func(), ok bool) {
	g, ok := s.Conn.(*smb.Group)
	if !ok {
		return nil, nil, false
	}
	if pool == smb.PoolBackground {
		c, done := g.Background()
		return c, done, true
	}
	return g.Interactive(), func() {}, true
}

// ThumbnailMemory is what the thumbnail budget holds now, and the budget.
func (a *API) ThumbnailMemory() (used, limit int64) {
	return a.thumbUsed.Load(), a.cfg.ThumbnailMemory
}

// errAttrs describes an error for the log without its text. An error from
// internal/smb is its kind and NT status; anything else only its Go type.
// Error texts from net/http, url, the body or a path check can carry a file
// name, a path or a query, and file names stay out of the log (CLAUDE.md).
func errAttrs(err error) []any {
	var e *smb.Error
	if errors.As(err, &e) {
		return []any{"error_kind", e.Kind.String(), "nt_status", fmt.Sprintf("0x%08x", e.Status)}
	}
	return []any{"error_type", fmt.Sprintf("%T", err)}
}

// drain reads what is left of a body, up to a bound, so the connection can
// be reused.
func drain(r io.Reader) { _, _ = io.CopyN(io.Discard, r, 64<<10) }
