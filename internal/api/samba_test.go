package api

// The API end to end against the test Samba (hack/samba): a browser's view of
// ferry. Skipped without FERRY_TEST_SMB_SERVER.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
	"github.com/denisneuling/ferry/internal/smb"
)

const alexPassword = "alex-test-password"

func sambaHarness(t testing.TB) *harness {
	t.Helper()
	addr := os.Getenv("FERRY_TEST_SMB_SERVER")
	if addr == "" {
		t.Skip("FERRY_TEST_SMB_SERVER is not set (make dev-samba)")
	}
	cfg := testConfig()
	cfg.SMBServer = addr
	cfg.MaxUpload = 8 << 20
	// As deployed: every test runs on a group of three connections.
	cfg.SMBConnections = 3
	return newHarness(t, cfg, smb.LoginGroup)
}

func (h *harness) login(t testing.TB, user, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(v1.LoginRequest{Username: user, Password: password})
	res := h.do(t, "POST", "/api/v1/login", bytes.NewReader(body), ferryReq, nil)
	if res.StatusCode != 200 {
		t.Fatalf("login as %s: %d", user, res.StatusCode)
	}
	for _, c := range res.Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func decode[T any](t testing.TB, res *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatalf("decoding %T (status %d): %v", v, res.StatusCode, err)
	}
	return v
}

func q(p string) string { return url.QueryEscape(p) }

func TestEndToEnd(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)

	me := decode[v1.Me](t, h.do(t, "GET", "/api/v1/me", nil, nil, c))
	if me.Username != "alex" || me.Server != "nas.example.lan" {
		t.Fatalf("me %+v", me)
	}

	shares := decode[v1.ShareList](t, h.do(t, "GET", "/api/v1/shares", nil, nil, c))
	if len(shares.Shares) != 5 || shares.Shares[0].Name != "alex" {
		t.Fatalf("shares %+v", shares)
	}

	// A folder of the test's own.
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "api-" + hex.EncodeToString(b)
	res := h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	if res.StatusCode != 201 {
		t.Fatalf("create folder: %d", res.StatusCode)
	}
	if e := decode[v1.Entry](t, res); e.Path != dir || e.Type != v1.EntryTypeFolder {
		t.Fatalf("folder %+v", e)
	}
	// The test logs out at its end, so the cleanup signs in afresh.
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, h.login(t, "alex", alexPassword))
	})

	// Upload, then download whole and in a range.
	content := strings.Repeat("0123456789", 300_000) // 3 MB, several chunks
	res = h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/digits.txt"), strings.NewReader(content), ferryReq, c)
	if res.StatusCode != 201 {
		t.Fatalf("upload: %d", res.StatusCode)
	}
	if e := decode[v1.Entry](t, res); e.Path != dir+"/digits.txt" || e.Size != int64(len(content)) {
		t.Fatalf("uploaded %+v", e)
	}
	res = h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/digits.txt"), strings.NewReader("x"), ferryReq, c)
	if res.StatusCode != 409 || problemCode(t, res) != "exists" {
		t.Fatalf("upload without overwrite: %d", res.StatusCode)
	}

	res = h.do(t, "GET", "/api/v1/shares/alex/content?path="+q(dir+"/digits.txt"), nil, nil, c)
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(got) != content {
		t.Fatalf("download: %d, %d bytes", res.StatusCode, len(got))
	}
	for k, want := range map[string]string{
		"Content-Disposition":     `attachment; filename="digits.txt"; filename*=UTF-8''digits.txt`,
		"Content-Security-Policy": contentCSP,
		"X-Content-Type-Options":  "nosniff",
		"Accept-Ranges":           "bytes",
		"Content-Length":          fmt.Sprint(len(content)),
	} {
		if g := res.Header.Get(k); g != want {
			t.Errorf("%s: %q, want %q", k, g, want)
		}
	}
	tag := res.Header.Get("ETag")

	res = h.do(t, "GET", "/api/v1/shares/alex/content?path="+q(dir+"/digits.txt"), nil, map[string]string{"Range": "bytes=1048570-1048579", "If-Range": tag}, c)
	got, _ = io.ReadAll(res.Body)
	if res.StatusCode != 206 || string(got) != content[1048570:1048580] || res.Header.Get("Content-Range") != fmt.Sprintf("bytes 1048570-1048579/%d", len(content)) {
		t.Fatalf("range: %d %q %s", res.StatusCode, got, res.Header.Get("Content-Range"))
	}
	res = h.do(t, "GET", "/api/v1/shares/alex/content?path="+q(dir+"/digits.txt"), nil, map[string]string{"Range": "bytes=0-9", "If-Range": `"stale"`}, c)
	if res.StatusCode != 200 {
		t.Fatalf("range with a stale If-Range: %d", res.StatusCode)
	}
	res = h.do(t, "GET", "/api/v1/shares/alex/content?path="+q(dir+"/digits.txt"), nil, map[string]string{"Range": "bytes=99999999-"}, c)
	if res.StatusCode != 416 || problemCode(t, res) != "range_not_satisfiable" {
		t.Fatalf("unsatisfiable range: %d", res.StatusCode)
	}
	res = h.do(t, "GET", "/api/v1/shares/alex/content?path="+q(dir+"/digits.txt"), nil, map[string]string{"If-None-Match": tag}, c)
	if res.StatusCode != 304 {
		t.Fatalf("If-None-Match: %d", res.StatusCode)
	}

	// An uploaded HTML file, asked for inline, is shown as text in a sandbox.
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/evil.html"), strings.NewReader("<script>alert(1)</script>"), ferryReq, c)
	res = h.do(t, "GET", "/api/v1/shares/alex/content?disposition=inline&path="+q(dir+"/evil.html"), nil, nil, c)
	if ct := res.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" || res.Header.Get("Content-Security-Policy") != contentCSP {
		t.Fatalf("HTML inline: %q", ct)
	}

	// Over the upload limit: refused, and nothing left behind.
	res = h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/big.bin"), bytes.NewReader(make([]byte, 9<<20)), ferryReq, c)
	if res.StatusCode != 413 {
		t.Fatalf("oversized upload: %d", res.StatusCode)
	}

	// Rename, list, delete.
	mv, _ := json.Marshal(v1.MoveRequest{From: dir + "/digits.txt", To: dir + "/Zahlen.txt"})
	res = h.do(t, "POST", "/api/v1/shares/alex/move", bytes.NewReader(mv), ferryReq, c)
	if res.StatusCode != 200 {
		t.Fatalf("move: %d", res.StatusCode)
	}
	list := decode[v1.EntryList](t, h.do(t, "GET", "/api/v1/shares/alex/entries?path="+q(dir), nil, nil, c))
	var names []string
	for _, e := range list.Entries {
		names = append(names, e.Name)
	}
	if list.Path != dir || len(names) != 2 || !strings.Contains(strings.Join(names, ","), "Zahlen.txt") {
		t.Fatalf("listing %+v", list)
	}
	res = h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(dir), nil, ferryReq, c)
	if res.StatusCode != 409 || problemCode(t, res) != "not_empty" {
		t.Fatalf("delete a full folder: %d", res.StatusCode)
	}

	// What Samba refuses.
	for _, c2 := range []struct {
		method, path string
		status       int
		code         string
	}{
		{"GET", "/api/v1/shares/sam/entries", 403, "access_denied"},
		{"GET", "/api/v1/shares/nosuch/entries", 404, "not_found"},
		{"GET", "/api/v1/shares/alex/entries?path=..%2Fsam", 400, "invalid_path"},
		{"GET", "/api/v1/shares/alex/content?path=Photos", 409, "is_a_folder"},
		{"PUT", "/api/v1/shares/Scans/content?path=x.txt", 403, "access_denied"},
	} {
		res := h.do(t, c2.method, c2.path, strings.NewReader("x"), ferryReq, c)
		if res.StatusCode != c2.status || problemCode(t, res) != c2.code {
			t.Errorf("%s %s: %d", c2.method, c2.path, res.StatusCode)
		}
	}

	// Logout ends the SMB session too.
	if res := h.do(t, "POST", "/api/v1/logout", nil, ferryReq, c); res.StatusCode != 204 {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/me", nil, nil, c); res.StatusCode != 401 {
		t.Fatalf("after logout: %d", res.StatusCode)
	}
	if h.tab.Len() != 0 {
		t.Fatalf("%d sessions after logout", h.tab.Len())
	}
}

func TestSharesAllowList(t *testing.T) {
	h := sambaHarness(t)
	h.api.cfg.Shares = []string{"alex", "family"}
	c := h.login(t, "alex", alexPassword)
	shares := decode[v1.ShareList](t, h.do(t, "GET", "/api/v1/shares", nil, nil, c))
	if len(shares.Shares) != 2 || shares.Shares[1].Name != "Family" {
		t.Fatalf("shares %+v", shares)
	}
	if res := h.do(t, "GET", "/api/v1/shares/Public/entries", nil, nil, c); res.StatusCode != 404 {
		t.Fatalf("a share outside the list: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/shares/hidden$/entries", nil, nil, c); res.StatusCode != 404 {
		t.Fatalf("a hidden share outside the list: %d", res.StatusCode)
	}
}

// A lost background connection leaves the person logged in: thumbnails and
// content go over what is left of the group, the interactive connection in
// the end.
func TestLostBackgroundConnection(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	s, ok := h.tab.Acquire(c.Value)
	if !ok {
		t.Fatal("no session")
	}
	g := s.Conn.(*smb.Group)
	if g.Size() != 3 {
		t.Fatalf("a group of %d", g.Size())
	}
	for range 2 {
		b, done := g.Background()
		done()
		_ = b.Close()
	}
	h.tab.Release(s)
	if res := h.do(t, "GET", "/api/v1/shares/alex/content?path=big.bin", nil, map[string]string{"Range": "bytes=0-99"}, c); res.StatusCode != 206 {
		t.Fatalf("content with no background connection left: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/me", nil, nil, c); res.StatusCode != 200 {
		t.Fatalf("logged out: %d", res.StatusCode)
	}
	if g.Size() != 1 {
		t.Fatalf("a group of %d", g.Size())
	}
}

// A lost SMB connection ends the ferry session with a 502, and the next
// request is a 401: nothing reconnects.
func TestLostSession(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	s, ok := h.tab.Acquire(c.Value)
	if !ok {
		t.Fatal("no session")
	}
	// Close the SMB session underneath, as a dropped connection would.
	_ = s.Conn.(*smb.Group).Interactive().Close()
	h.tab.Release(s)
	res := h.do(t, "GET", "/api/v1/shares", nil, nil, c)
	if res.StatusCode != 401 && res.StatusCode != 502 {
		t.Fatalf("after the drop: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/me", nil, nil, c); res.StatusCode != 401 {
		t.Fatalf("still logged in: %d", res.StatusCode)
	}
}

// Neither the password nor the username reaches ferry's log, whatever the
// login's outcome.
func TestCredentialsNeverLogged(t *testing.T) {
	h := sambaHarness(t)
	const sentinel = "S3ntinel-Pa55-91ab"
	for _, cred := range []v1.LoginRequest{
		{Username: "alex", Password: sentinel},                  // wrong password
		{Username: "nosuchuser-" + sentinel, Password: "x"},     // mapped to guest
		{Username: "alex", Password: alexPassword},              // success
		{Username: "WORKGROUP\\alex", Password: alexPassword},   // with a domain
		{Username: "alex", Password: strings.Repeat("p", 1025)}, // refused before SMB
	} {
		body, _ := json.Marshal(cred)
		h.do(t, "POST", "/api/v1/login", bytes.NewReader(body), ferryReq, nil)
	}
	log := h.log.String()
	if !strings.Contains(log, `"outcome":"guest"`) || !strings.Contains(log, `"outcome":"ok"`) {
		t.Fatalf("the log lacks the outcomes:\n%s", log)
	}
	for _, secret := range []string{sentinel, alexPassword, "nosuchuser", "alex", "WORKGROUP"} {
		if strings.Contains(log, secret) {
			t.Errorf("%q appears in the log:\n%s", secret, log)
		}
	}
}

// BenchmarkDownload measures a download through the API, so that the 1 MiB
// copy (copyOut) stays: net/http's own copy would read 32 KiB at a time.
func BenchmarkDownload(b *testing.B) {
	h := sambaHarness(b)
	c := h.login(b, "alex", alexPassword)
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/shares/alex/content?path=big.bin", nil)
		req.AddCookie(c)
		res, err := h.srv.Client().Do(req)
		if err != nil {
			b.Fatal(err)
		}
		n, err := io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			b.Fatal(res.StatusCode, err)
		}
		b.SetBytes(n)
	}
}

func TestThumbnail(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "thumb-" + hex.EncodeToString(b)
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c)
	})

	img := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, img, nil); err != nil {
		t.Fatal(err)
	}
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/photo.jpg"), bytes.NewReader(photo.Bytes()), ferryReq, c)
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/notes.txt"), strings.NewReader("not an image"), ferryReq, c)

	res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=160&path="+q(dir+"/photo.jpg"), nil, nil, c)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("thumbnail: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	th, err := jpeg.Decode(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b := th.Bounds(); b.Dx() != 160 || b.Dy() != 106 {
		t.Fatalf("thumbnail %v", b)
	}
	tag := res.Header.Get("ETag")
	if res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=160&path="+q(dir+"/photo.jpg"), nil, map[string]string{"If-None-Match": tag}, c); res.StatusCode != 304 {
		t.Fatalf("revalidation: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=320&path="+q(dir+"/photo.jpg"), nil, map[string]string{"If-None-Match": tag}, c); res.StatusCode != 200 {
		t.Fatalf("another size under the same ETag: %d", res.StatusCode)
	}

	for _, c2 := range []struct {
		path   string
		status int
		code   string
	}{
		{"/api/v1/shares/alex/thumbnail?path=" + q(dir+"/notes.txt"), 415, "not_an_image"},
		{"/api/v1/shares/alex/thumbnail?path=" + q(dir), 409, "is_a_folder"},
		{"/api/v1/shares/alex/thumbnail?path=" + q(dir+"/nope.jpg"), 404, "not_found"},
		{"/api/v1/shares/alex/thumbnail?size=100&path=" + q(dir+"/photo.jpg"), 400, "invalid_request"},
	} {
		res := h.do(t, "GET", c2.path, nil, nil, c)
		if res.StatusCode != c2.status || problemCode(t, res) != c2.code {
			t.Errorf("%s: %d", c2.path, res.StatusCode)
		}
	}

	// A recording's thumbnail is its cover: an MP3 whose ID3 tag carries the
	// photo, before 4 MiB of what stands for sound — more than the byte
	// bound below allows for the file, not for the cover.
	sound := bytes.Repeat([]byte{0xFF, 0xFB, 0x90, 0x00}, 1<<20)
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/song.mp3"), bytes.NewReader(append(id3WithCover(photo.Bytes()), sound...)), ferryReq, c)
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/bare.mp3"), bytes.NewReader(append(id3WithCover(nil), sound...)), ferryReq, c)
	h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/unsync.mp3"), bytes.NewReader(append(id3UnsyncCover(photo.Bytes()), sound...)), ferryReq, c)
	h.api.cfg.ThumbnailMaxBytes = int64(photo.Len() + 1)
	for _, f := range []string{"song.mp3", "unsync.mp3", "unsync.mp3"} {
		res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?size=160&path="+q(dir+"/"+f), nil, nil, c)
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" {
			t.Fatalf("%s's cover: %d", f, res.StatusCode)
		}
		if th, err := jpeg.Decode(res.Body); err != nil || th.Bounds().Dx() != 160 {
			t.Fatalf("%s's thumbnail: %v", f, err)
		}
	}
	// The cover slot is given back: every slot is free again.
	if n := len(h.api.coverSlots); n != 0 {
		t.Fatalf("%d cover slots still held", n)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?path="+q(dir+"/bare.mp3"), nil, nil, c); res.StatusCode != 415 || problemCode(t, res) != "not_an_image" {
		t.Fatalf("a recording without a cover: %d", res.StatusCode)
	}

	h.api.cfg.ThumbnailMaxBytes = int64(photo.Len() - 1)
	for _, f := range []string{"photo.jpg", "song.mp3"} {
		res := h.do(t, "GET", "/api/v1/shares/alex/thumbnail?path="+q(dir+"/"+f), nil, nil, c)
		if res.StatusCode != 413 || problemCode(t, res) != "too_large" {
			t.Fatalf("%s over the byte bound: %d", f, res.StatusCode)
		}
	}
	res = h.do(t, "GET", "/api/v1/shares/alex/thumbnail?path="+q(dir+"/photo.jpg"), nil, nil, c)
	if res.StatusCode != 413 || problemCode(t, res) != "too_large" {
		t.Fatalf("over the byte bound: %d", res.StatusCode)
	}

	// An image whose estimated cost exceeds the memory budget is refused
	// before it is read.
	h.api.cfg.ThumbnailMaxBytes = 8 << 20
	h.api.cfg.ThumbnailMemory = 1 << 20
	res = h.do(t, "GET", "/api/v1/shares/alex/thumbnail?path="+q(dir+"/photo.jpg"), nil, nil, c)
	if res.StatusCode != 413 || problemCode(t, res) != "too_large" {
		t.Fatalf("over the memory budget: %d", res.StatusCode)
	}
}

// id3WithCover is an ID3v2.3 tag with a title and, given one, a picture as
// the front cover.
func id3WithCover(pic []byte) []byte {
	frame := func(id string, body []byte) []byte {
		return append(append([]byte(id), byte(len(body)>>24), byte(len(body)>>16), byte(len(body)>>8), byte(len(body)), 0, 0), body...)
	}
	body := frame("TIT2", []byte("\x00A song"))
	if pic != nil {
		body = append(body, frame("APIC", append([]byte("\x00image/jpeg\x00\x03Front\x00"), pic...))...)
	}
	n := len(body)
	return append([]byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}, body...)
}

// id3UnsyncCover is an ID3v2.4 tag whose picture frame is written with
// unsynchronisation: ferry undoes it in memory, through the cover gate.
func id3UnsyncCover(pic []byte) []byte {
	data := append([]byte("\x00image/jpeg\x00\x03\x00"), pic...)
	var u []byte
	for i, c := range data {
		u = append(u, c)
		if c == 0xFF && (i+1 == len(data) || data[i+1]&0xE0 == 0xE0 || data[i+1] == 0) {
			u = append(u, 0)
		}
	}
	ss := func(n int) []byte {
		return []byte{byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
	}
	frame := append(append([]byte("APIC"), ss(len(u))...), 0, 0x02)
	frame = append(frame, u...)
	return append(append([]byte{'I', 'D', '3', 4, 0, 0}, ss(len(frame))...), frame...)
}

// A login that fails, sent with the cookie of a running session, leaves that
// session alone: only a successful login replaces it.
func TestFailedLoginKeepsTheSession(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	body, _ := json.Marshal(v1.LoginRequest{Username: "alex", Password: "wrong"})
	if res := h.do(t, "POST", "/api/v1/login", bytes.NewReader(body), ferryReq, c); res.StatusCode != 401 {
		t.Fatalf("wrong password: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/me", nil, nil, c); res.StatusCode != 200 {
		t.Fatalf("the session after a failed login: %d", res.StatusCode)
	}
}

// A browser abandons requests all the time — thumbnails scrolled away, a
// folder left before its listing came, a video seeked. Each such request
// ends in the middle of its SMB call; the session must outlive all of them.
func TestAbortedRequestsKeepTheSession(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	paths := []string{
		"/api/v1/shares/alex/entries",
		"/api/v1/shares/alex/content?path=big.bin",
		"/api/v1/shares/alex/content?path=hello.txt",
		"/api/v1/shares/alex/thumbnail?path=hello.txt",
		"/api/v1/shares",
	}
	var wg sync.WaitGroup
	for i := 0; i < 800; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(i%7)*300*time.Microsecond)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", h.srv.URL+paths[i%len(paths)], nil)
			req.AddCookie(c)
			if res, err := h.srv.Client().Do(req); err == nil {
				_, _ = io.CopyN(io.Discard, res.Body, 2<<20)
				_ = res.Body.Close()
			}
		}()
		if i%25 == 0 {
			wg.Wait()
		}
	}
	wg.Wait()
	for i := 0; i < 10; i++ {
		if res := h.do(t, "GET", "/api/v1/shares/alex/entries", nil, nil, c); res.StatusCode != 200 {
			t.Fatalf("the session after aborted requests: %d", res.StatusCode)
		}
	}
}

// TestSpaceEndpoint: a share's free space as JSON, and Samba's refusal of
// someone else's share as the usual problem.
func TestSpaceEndpoint(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	res := h.do(t, "GET", "/api/v1/shares/alex/space?path=Photos", nil, nil, c)
	sp := decode[v1.Space](t, res)
	if res.StatusCode != 200 || sp.Total <= 0 || sp.Free <= 0 || sp.Free > sp.Total {
		t.Fatalf("status %d, space %+v", res.StatusCode, sp)
	}
	if res := h.do(t, "GET", "/api/v1/shares/sam/space", nil, nil, c); res.StatusCode != 403 || problemCode(t, res) != "access_denied" {
		t.Fatalf("sam's share: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/space?path=..%2Fsam", nil, nil, c); res.StatusCode != 400 {
		t.Fatalf("an invalid path: %d", res.StatusCode)
	}
}
