package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/config"
	"github.com/metapatrol/ferry/internal/session"
	"github.com/metapatrol/ferry/internal/smb"
)

func testConfig() config.Config {
	return config.Config{
		SMBServer:            "127.0.0.1:1",
		ServerName:           "nas.example.lan",
		IdleTimeout:          30 * time.Minute,
		SessionLifetime:      12 * time.Hour,
		MaxSessions:          10,
		LoginTimeout:         5 * time.Second,
		RequestTimeout:       10 * time.Second,
		StallTimeout:         10 * time.Second,
		MaxListing:           1000,
		MaxDelete:            1000,
		MaxArchive:           1000,
		LoginRatePerAddress:  100,
		LoginRatePerUser:     3,
		CookieSecure:         "auto",
		ThumbnailMaxBytes:    8 << 20,
		ThumbnailMaxPixels:   50_000_000,
		ThumbnailConcurrency: 2,
		ThumbnailMemory:      320 << 20,
	}
}

// harness is the API behind an HTTP server, with its log captured.
type harness struct {
	srv *httptest.Server
	log *bytes.Buffer
	api *API
	tab *session.Table
}

func newHarness(t testing.TB, cfg config.Config, login LoginFunc) *harness {
	t.Helper()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tab := session.New(session.Options{IdleTimeout: cfg.IdleTimeout, Lifetime: cfg.SessionLifetime, MaxSessions: cfg.MaxSessions})
	a := New(cfg, tab, login, log, "test", nil)
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(func() { srv.Close(); tab.CloseAll() })
	return &harness{srv: srv, log: &buf, api: a, tab: tab}
}

func (h *harness) do(t testing.TB, method, path string, body io.Reader, hdr map[string]string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	// The whole body is read before the next request goes out. A body left
	// unread keeps its handler writing — and its file open on the share,
	// which makes Samba refuse a rename of that file as a sharing violation.
	// On loopback the socket buffers can hide that; on a CI runner they did
	// not.
	b, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	return res
}

var ferryReq = map[string]string{"X-Ferry-Request": "1", "Content-Type": "application/json"}

func problemCode(t testing.TB, res *http.Response) string {
	t.Helper()
	if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type %q, status %d", ct, res.StatusCode)
	}
	var p v1.Problem
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Status != res.StatusCode {
		t.Fatalf("problem status %d, response %d", p.Status, res.StatusCode)
	}
	return p.Code
}

func refuse(kind smb.Kind) LoginFunc {
	return func(context.Context, smb.LoginOptions, smb.Credentials) (*smb.Group, error) {
		return nil, &smb.Error{Kind: kind, Status: 0xC000006D}
	}
}

func TestInfo(t *testing.T) {
	h := newHarness(t, testConfig(), refuse(smb.KindAuth))
	res := h.do(t, "GET", "/api/v1/info", nil, nil, nil)
	var info v1.Info
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Server != "nas.example.lan" || info.IdleTimeoutSeconds != 1800 || info.Version != "test" {
		t.Fatalf("info %+v", info)
	}
	if info.Limits.Listing != 1000 || info.Limits.Delete != 1000 || info.Limits.Upload != 0 {
		t.Fatalf("limits %+v", info.Limits)
	}
	for k, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		"Cache-Control":           "no-store",
	} {
		if got := res.Header.Get(k); got != want {
			t.Errorf("%s: %q", k, got)
		}
	}
}

func TestFerryRequestHeader(t *testing.T) {
	h := newHarness(t, testConfig(), refuse(smb.KindAuth))
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/login"},
		{"POST", "/api/v1/logout"},
		{"PUT", "/api/v1/shares/alex/content?path=a.txt"},
		{"POST", "/api/v1/shares/alex/folders?path=New"},
		{"POST", "/api/v1/shares/alex/move"},
		{"DELETE", "/api/v1/shares/alex/entries?path=a.txt"},
	} {
		res := h.do(t, c.method, c.path, strings.NewReader("{}"), map[string]string{"Content-Type": "application/json"}, nil)
		if res.StatusCode != 400 || problemCode(t, res) != "missing_ferry_request" {
			t.Errorf("%s %s: %d", c.method, c.path, res.StatusCode)
		}
	}
	res := h.do(t, "POST", "/api/v1/logout", nil, map[string]string{"X-Ferry-Request": "0"}, nil)
	if res.StatusCode != 400 {
		t.Errorf("X-Ferry-Request: 0 accepted (%d)", res.StatusCode)
	}
}

func TestUnauthenticated(t *testing.T) {
	h := newHarness(t, testConfig(), refuse(smb.KindAuth))
	bogus := &http.Cookie{Name: cookieName, Value: "not-a-session"}
	for _, c := range []struct {
		method, path string
		cookie       *http.Cookie
	}{
		{"GET", "/api/v1/me", nil},
		{"GET", "/api/v1/shares", bogus},
		{"GET", "/api/v1/shares/alex/entries", nil},
		{"GET", "/api/v1/shares/alex/content?path=a.txt", bogus},
		{"DELETE", "/api/v1/shares/alex/entries?path=a.txt", nil},
	} {
		res := h.do(t, c.method, c.path, nil, ferryReq, c.cookie)
		if res.StatusCode != 401 || problemCode(t, res) != "unauthenticated" {
			t.Errorf("%s %s: %d", c.method, c.path, res.StatusCode)
		}
	}
}

func TestLoginRefused(t *testing.T) {
	cases := []struct {
		kind   smb.Kind
		status int
		code   string
	}{
		{smb.KindAuth, 401, "login_failed"},
		{smb.KindGuest, 401, "login_failed"},
		{smb.KindUnreachable, 502, "server_unreachable"},
	}
	for _, c := range cases {
		h := newHarness(t, testConfig(), refuse(c.kind))
		res := h.do(t, "POST", "/api/v1/login", strings.NewReader(`{"username":"alex","password":"pw"}`), ferryReq, nil)
		if res.StatusCode != c.status || problemCode(t, res) != c.code {
			t.Errorf("%s: %d", c.kind, res.StatusCode)
		}
		if len(res.Cookies()) != 0 {
			t.Errorf("%s: a cookie was set", c.kind)
		}
	}
}

func TestLoginRequestValidation(t *testing.T) {
	h := newHarness(t, testConfig(), refuse(smb.KindAuth))
	for _, body := range []string{
		``, `{`, `[]`, `{"username":"alex"}`, `{"username":"","password":"x"}`,
		`{"username":"alex","password":"x","extra":1}`, `{"username":"alex","password":"x"}{}`,
		`{"username":"` + strings.Repeat("a", 300) + `","password":"x"}`,
	} {
		res := h.do(t, "POST", "/api/v1/login", strings.NewReader(body), ferryReq, nil)
		if res.StatusCode != 400 {
			t.Errorf("%q: %d", body, res.StatusCode)
		}
	}
	res := h.do(t, "POST", "/api/v1/login", strings.NewReader(`{"username":"alex","password":"`+strings.Repeat("x", 8<<10)+`"}`), ferryReq, nil)
	if res.StatusCode != 413 {
		t.Errorf("oversized body: %d", res.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	cfg := testConfig()
	cfg.LoginRatePerUser = 3
	cfg.LoginRatePerAddress = 5
	h := newHarness(t, cfg, refuse(smb.KindAuth))
	login := func(user string) *http.Response {
		return h.do(t, "POST", "/api/v1/login", strings.NewReader(`{"username":"`+user+`","password":"pw"}`), ferryReq, nil)
	}
	for i := 0; i < 3; i++ {
		if res := login("alex"); res.StatusCode != 401 {
			t.Fatalf("attempt %d: %d", i, res.StatusCode)
		}
	}
	res := login("ALEX")
	if res.StatusCode != 429 || res.Header.Get("Retry-After") == "" || problemCode(t, res) != "rate_limited" {
		t.Fatalf("fourth attempt for the user: %d", res.StatusCode)
	}
	// The address allows five, refused attempts included: one more for
	// another user, then none.
	if res := login("sam"); res.StatusCode != 401 {
		t.Fatalf("other user: %d", res.StatusCode)
	}
	if res := login("kim"); res.StatusCode != 429 {
		t.Fatalf("address limit: %d", res.StatusCode)
	}
}

func TestLogoutWithoutSession(t *testing.T) {
	h := newHarness(t, testConfig(), refuse(smb.KindAuth))
	res := h.do(t, "POST", "/api/v1/logout", nil, ferryReq, &http.Cookie{Name: cookieName, Value: "x"})
	if res.StatusCode != 204 {
		t.Fatalf("status %d", res.StatusCode)
	}
	// Thumbnails and details are cached for a year; they leave with it.
	if got := res.Header.Get("Clear-Site-Data"); got != `"cache"` {
		t.Errorf("Clear-Site-Data %q", got)
	}
	found := false
	for _, c := range res.Cookies() {
		if c.Name == cookieName && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("cookie not cleared")
	}
}

func TestClientAddr(t *testing.T) {
	cfg := testConfig()
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	a := New(cfg, nil, nil, slog.Default(), "", nil)
	cases := []struct{ remote, xff, want string }{
		{"198.51.100.7:5000", "203.0.113.9", "198.51.100.7"},           // untrusted peer: its header is ignored
		{"192.0.2.1:5000", "203.0.113.9", "203.0.113.9"},               // trusted proxy
		{"192.0.2.1:5000", "203.0.113.66, 203.0.113.9", "203.0.113.9"}, // the nearest untrusted hop, not the client's claim
		{"192.0.2.1:5000", "203.0.113.9, 192.0.2.2", "203.0.113.9"},    // through two proxies
		{"192.0.2.1:5000", "", "192.0.2.1"},
		{"192.0.2.1:5000", "garbage", "192.0.2.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := a.clientAddr(r); got != c.want {
			t.Errorf("%s / %q: %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
}

func TestSecureCookie(t *testing.T) {
	cfg := testConfig()
	a := New(cfg, nil, nil, slog.Default(), "", nil)
	r := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	a.setCookie(w, r, "id")
	c := w.Result().Cookies()[0]
	if c.Name != cookieName || c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("plain HTTP cookie %+v", c)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	a.setCookie(w, r, "id")
	if c := w.Result().Cookies()[0]; c.Name != secureCookieName || !c.Secure {
		t.Fatalf("HTTPS cookie %+v", c)
	}
}

func TestParseRange(t *testing.T) {
	cases := []struct {
		h                   string
		size, start, length int64
		ok, satisfiable     bool
	}{
		{"bytes=0-99", 1000, 0, 100, true, true},
		{"bytes=500-", 1000, 500, 500, true, true},
		{"bytes=-100", 1000, 900, 100, true, true},
		{"bytes=-5000", 1000, 0, 1000, true, true},
		{"bytes=900-5000", 1000, 900, 100, true, true},
		{"bytes=1000-", 1000, 0, 0, false, false},
		{"bytes=-0", 1000, 0, 0, false, false},
		{"bytes=0-0", 0, 0, 0, false, false},
		{"bytes=0-1,5-6", 1000, 0, 0, false, true},
		{"items=0-1", 1000, 0, 0, false, true},
		{"bytes=5-1", 1000, 0, 0, false, true},
		{"bytes=a-b", 1000, 0, 0, false, true},
	}
	for _, c := range cases {
		s, l, ok, sat := parseRange(c.h, c.size)
		if ok != c.ok || sat != c.satisfiable || (ok && (s != c.start || l != c.length)) {
			t.Errorf("%q of %d: %d+%d ok=%v sat=%v", c.h, c.size, s, l, ok, sat)
		}
	}
}

func TestContentType(t *testing.T) {
	cases := []struct {
		name   string
		inline bool
		ctype  string
		asInl  bool
	}{
		{"photo.JPG", true, "image/jpeg", true},
		{"clip.mp4", true, "video/mp4", true},
		{"doc.pdf", true, "application/pdf", true},
		{"notes.md", true, "text/plain; charset=utf-8", true},
		{"page.html", true, "text/plain; charset=utf-8", true},
		{"logo.svg", true, "text/plain; charset=utf-8", true},
		{"page.html", false, "application/octet-stream", false},
		{"logo.svg", false, "application/octet-stream", false},
		{"data.xml", false, "application/octet-stream", false},
		{"app.js", false, "application/octet-stream", false},
		{"archive.zip", true, "application/zip", false},
		{"noextension", false, "application/octet-stream", false},
		{"photo.jpg", false, "image/jpeg", false},
	}
	for _, c := range cases {
		ct, inl := contentType(c.name, c.inline)
		if ct != c.ctype || inl != c.asInl {
			t.Errorf("%s inline=%v: %q %v", c.name, c.inline, ct, inl)
		}
	}
}

func TestDisposition(t *testing.T) {
	got := disposition("attachment", `Fotos "Sommer" ü;.jpg`)
	want := `attachment; filename="Fotos _Sommer_ __.jpg"; filename*=UTF-8''Fotos%20%22Sommer%22%20%C3%BC%3B.jpg`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestIfRange(t *testing.T) {
	mod := time.Date(2026, 11, 21, 12, 0, 0, 500, time.UTC)
	tag := etag(10, mod)
	if !ifRangeHolds("", tag, mod) || !ifRangeHolds(tag, tag, mod) || ifRangeHolds(`"other"`, tag, mod) {
		t.Fatal("etag forms")
	}
	if !ifRangeHolds(mod.Format(http.TimeFormat), tag, mod) || ifRangeHolds(mod.Add(-time.Hour).Format(http.TimeFormat), tag, mod) {
		t.Fatal("date forms")
	}
}
