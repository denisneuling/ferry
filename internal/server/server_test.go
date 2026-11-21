package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/metapatrol/ferry/internal/api"
	"github.com/metapatrol/ferry/internal/config"
	"github.com/metapatrol/ferry/internal/session"
	"github.com/metapatrol/ferry/internal/smb"
)

func newTestServer(t *testing.T, smbServer string, log *slog.Logger) *httptest.Server {
	t.Helper()
	cfg := config.Config{
		SMBServer: smbServer, ServerName: "nas.example.lan",
		ListenAddr: "127.0.0.1:0", OpsListenAddr: "127.0.0.1:0",
		IdleTimeout: 30 * time.Minute, SessionLifetime: 12 * time.Hour, MaxSessions: 10,
		LoginTimeout: 5 * time.Second, RequestTimeout: 10 * time.Second, StallTimeout: 10 * time.Second,
		MaxListing: 1000, LoginRatePerAddress: 100, LoginRatePerUser: 100, CookieSecure: "auto",
	}
	tab := session.New(session.Options{IdleTimeout: cfg.IdleTimeout, Lifetime: cfg.SessionLifetime, MaxSessions: cfg.MaxSessions})
	s := New(cfg, tab, api.New(cfg, tab, smb.Login, log, "test"), log)
	srv := httptest.NewServer(s.public.Handler)
	t.Cleanup(func() { srv.Close(); tab.CloseAll() })
	return srv
}

func TestHeadersAndRoot(t *testing.T) {
	srv := newTestServer(t, "127.0.0.1:1", slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("X-Frame-Options") != "DENY" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("root: %d %v", res.StatusCode, res.Header)
	}
	res, err = http.Get(srv.URL + "/api/v1/info")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.Header.Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'none'" {
		t.Fatalf("the API's CSP: %q", res.Header.Get("Content-Security-Policy"))
	}
}

// No file or folder name reaches the log, at the debug level either, through
// the whole stack and its request log: a folder and a file with distinctive
// names go through every operation and its errors.
func TestPathsNeverLogged(t *testing.T) {
	addr := os.Getenv("FERRY_TEST_SMB_SERVER")
	if addr == "" {
		t.Skip("FERRY_TEST_SMB_SERVER is not set (make dev-samba)")
	}
	var buf bytes.Buffer
	srv := newTestServer(t, addr, slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	client := srv.Client()
	ferry := map[string]string{"X-Ferry-Request": "1", "Content-Type": "application/json"}
	do := func(method, path, body string, hdr map[string]string, cookie *http.Cookie) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		return res
	}
	login, _ := json.Marshal(map[string]string{"username": "alex", "password": "alex-test-password"})
	res := do("POST", "/api/v1/login", string(login), ferry, nil)
	if res.StatusCode != 200 || len(res.Cookies()) == 0 {
		t.Fatalf("login: %d", res.StatusCode)
	}
	c := res.Cookies()[0]

	const dir, file = "Ordner-Pfadwaechter-77e1", "Datei-Pfadwaechter-77e1.txt"
	full := dir + "/" + file
	q := url.QueryEscape
	for _, s := range []struct{ method, path, body string }{
		{"POST", "/api/v1/shares/alex/folders?path=" + q(dir), ""},
		{"POST", "/api/v1/shares/alex/folders?path=" + q(dir), ""}, // exists
		{"PUT", "/api/v1/shares/alex/content?path=" + q(full), "secret plans"},
		{"PUT", "/api/v1/shares/alex/content?path=" + q(full), "again"}, // exists
		{"GET", "/api/v1/shares/alex/entries?path=" + q(dir), ""},
		{"GET", "/api/v1/shares/alex/content?path=" + q(full), ""},
		{"GET", "/api/v1/shares/alex/content?path=" + q(dir+"/missing-"+file), ""}, // not found
		{"GET", "/api/v1/shares/alex/content?path=" + q("../"+full), ""},           // invalid
		{"GET", "/api/v1/shares/sam/entries?path=" + q(dir), ""},                   // denied
		{"POST", "/api/v1/shares/alex/move", `{"from":"` + full + `","to":"` + dir + `/moved-` + file + `"}`},
		{"POST", "/api/v1/shares/alex/move", `{"from":"` + full + `","to":"` + dir + `/x"}`}, // gone
		{"POST", "/api/v1/shares/alex/move", `{"from":"` + dir + `","to":"` + dir + `/inside"}`},
		{"DELETE", "/api/v1/shares/alex/entries?path=" + q(dir), ""}, // not empty
		{"DELETE", "/api/v1/shares/alex/entries?recursive=true&path=" + q(dir), ""},
	} {
		do(s.method, s.path, s.body, ferry, c)
	}
	// Ranged and stalled-style answers through the same stack.
	do("GET", "/api/v1/shares/alex/content?path="+q("hello.txt"), "", map[string]string{"Range": "bytes=999-"}, c)

	log := buf.String()
	if strings.Count(log, `"route":"`) < 15 {
		t.Fatalf("the log lacks the request lines to check:\n%s", log)
	}
	for _, secret := range []string{"Pfadwaechter", "secret plans", "alex-test-password", `"alex"`} {
		if strings.Contains(log, secret) {
			t.Errorf("%q appears in the log:\n%s", secret, log)
		}
	}
}
