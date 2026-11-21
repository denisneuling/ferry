package smb

// Tests against a real Samba: hack/samba, started by `make dev-samba` and as a
// job service in CI. They read its address from FERRY_TEST_SMB_SERVER and skip
// without it. The users and passwords are hack/samba's test values.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	alexPassword = "alex-test-password"
	samPassword  = "sam-test-password"
)

func sambaServer(t testing.TB) string {
	t.Helper()
	addr := os.Getenv("FERRY_TEST_SMB_SERVER")
	if addr == "" {
		t.Skip("FERRY_TEST_SMB_SERVER is not set (make dev-samba)")
	}
	return addr
}

func login(t testing.TB, user, password string) (*Session, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return Login(ctx, LoginOptions{Server: sambaServer(t)}, Credentials{User: user, Password: password})
}

func mustLogin(t testing.TB, user, password string) *Session {
	t.Helper()
	s, err := login(t, user, password)
	if err != nil {
		t.Fatalf("login as %s: %v", user, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The hazard this package exists to hold: under `map to guest = Bad User`,
// go-smb2 on its own hands an unknown user a working guest session.
func TestLoginOutcomes(t *testing.T) {
	cases := []struct {
		name, user, password string
		want                 Kind
	}{
		{"wrong password", "alex", "not-the-password", KindAuth},
		{"unknown user, mapped to guest by the server", "nosuchuser", "whatever", KindGuest},
		{"the guest account itself", "nobody", "whatever", KindGuest},
		{"empty password", "alex", "", KindAuth},
		{"empty user", "", "whatever", KindAuth},
		{"anonymous", "", "", KindAuth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := login(t, tc.user, tc.password)
			if err == nil {
				_ = s.Close()
				t.Fatal("got a session")
			}
			if KindOf(err) != tc.want {
				t.Fatalf("got %v, want kind %s", err, tc.want)
			}
		})
	}
}

// Under `map to guest = Bad Password`, Samba answers a wrong password for an
// existing user with a guest session — the hazard in its plainest form. The
// second test instance (hack/samba/smb-bad-password.conf) runs that way;
// FERRY_TEST_SMB_SERVER_BAD_PASSWORD names it.
func TestLoginBadPasswordMappedToGuest(t *testing.T) {
	addr := os.Getenv("FERRY_TEST_SMB_SERVER_BAD_PASSWORD")
	if addr == "" {
		t.Skip("FERRY_TEST_SMB_SERVER_BAD_PASSWORD is not set (make dev-samba)")
	}
	ctx := context.Background()
	for _, c := range []struct {
		name, user, password string
		want                 Kind
	}{
		{"wrong password", "alex", "not-the-password", KindGuest},
		{"unknown user", "nosuchuser", "whatever", KindGuest},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := Login(ctx, LoginOptions{Server: addr}, Credentials{User: c.user, Password: c.password})
			if err == nil {
				_ = s.Close()
				t.Fatal("got a session")
			}
			if KindOf(err) != c.want {
				t.Fatalf("got %v, want kind %s", err, c.want)
			}
		})
	}
	// The right password still logs in on that instance.
	s, err := Login(ctx, LoginOptions{Server: addr}, Credentials{User: "alex", Password: alexPassword})
	if err != nil {
		t.Fatalf("right password: %v", err)
	}
	_ = s.Close()
}

func TestLoginUnreachable(t *testing.T) {
	sambaServer(t)
	ctx := context.Background()
	_, err := Login(ctx, LoginOptions{Server: "127.0.0.1:1", Timeout: 2 * time.Second}, Credentials{User: "alex", Password: alexPassword})
	if KindOf(err) != KindUnreachable {
		t.Fatalf("got %v, want unreachable", err)
	}
}

func TestShares(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	shares, err := s.Shares(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, sh := range shares {
		names = append(names, sh.Name)
	}
	// Samba enumerates every browseable share, including sam's, which alex
	// cannot open; hidden$ and IPC$ are left out by ferry.
	want := "alex,Family,Public,sam,Scans"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("shares %s, want %s", got, want)
	}
	if shares[0].Comment != "Alex's files" {
		t.Errorf("comment %q", shares[0].Comment)
	}
}

// Samba decides: a listed share alex may not open is refused when opened.
func TestShareAccessIsSambas(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	if _, err := s.ReadDir(ctx, "sam", "", 100); KindOf(err) != KindAccessDenied {
		t.Fatalf("sam's share: got %v, want access denied", err)
	}
	if _, err := s.ReadDir(ctx, "nosuchshare", "", 100); KindOf(err) != KindNotFound {
		t.Fatalf("missing share: got %v, want not found", err)
	}
	// Hidden from the listing, but Samba lets alex in, so ferry does too.
	if _, err := s.ReadDir(ctx, "hidden$", "", 100); err != nil {
		t.Fatalf("hidden$: %v", err)
	}
}

func TestReadDirAndOpen(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	entries, err := s.ReadDir(ctx, "alex", "/", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range entries {
		got[e.Name] = e
	}
	if !got["Photos"].Dir || got["hello.txt"].Dir || got["hello.txt"].Size != 16 {
		t.Fatalf("entries %+v", entries)
	}

	f, err := s.Open(ctx, "alex", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil || string(b) != "hello from alex\n" {
		t.Fatalf("content %q, %v", b, err)
	}

	// Range: what http.ServeContent will do.
	f, err = s.Open(ctx, "alex", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 4)
	if n, err := f.ReadAt(buf, 6); err != nil || string(buf[:n]) != "from" {
		t.Fatalf("ReadAt %q, %v", buf[:n], err)
	}
	if _, err := f.Seek(11, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(f); string(b) != "alex\n" {
		t.Fatalf("after seek %q", b)
	}

	if _, err := s.Open(ctx, "alex", "Photos"); KindOf(err) != KindIsDir {
		t.Fatalf("open folder: %v", err)
	}
	if _, err := s.Open(ctx, "alex", "nope.txt"); KindOf(err) != KindNotFound {
		t.Fatalf("open missing: %v", err)
	}
	if _, err := s.ReadDir(ctx, "alex", "hello.txt", 100); err == nil {
		t.Fatal("listed a file as a folder")
	}
	if _, err := s.Open(ctx, "alex", "../sam/private.txt"); KindOf(err) != KindInvalidPath {
		t.Fatalf("traversal: %v", err)
	}
	if _, err := s.ReadDir(ctx, "alex", "", 1); KindOf(err) != KindLimit {
		t.Fatalf("listing bound: %v", err)
	}
}

// Two people's sessions stay apart under concurrency: every operation runs
// with the identity of the session it was made on.
func TestSessionsDoNotCross(t *testing.T) {
	alex := mustLogin(t, "alex", alexPassword)
	sam := mustLogin(t, "sam", samPassword)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := alex.ReadDir(ctx, "sam", "", 100); KindOf(err) != KindAccessDenied {
				errs <- fmt.Errorf("alex read sam's share: %v", err)
			}
			if _, err := alex.ReadDir(ctx, "alex", "", 100); err != nil {
				errs <- fmt.Errorf("alex lost his own share: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := sam.ReadDir(ctx, "alex", "", 100); KindOf(err) != KindAccessDenied {
				errs <- fmt.Errorf("sam read alex's share: %v", err)
			}
			if _, err := sam.ReadDir(ctx, "sam", "", 100); err != nil {
				errs <- fmt.Errorf("sam lost her own share: %v", err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// A dropped connection ends the session; nothing reconnects.
func TestDroppedConnectionEndsTheSession(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	if _, err := s.ReadDir(ctx, "alex", "", 100); err != nil {
		t.Fatal(err)
	}
	_ = s.conn.Close()
	if _, err := s.ReadDir(ctx, "alex", "", 100); KindOf(err) != KindSessionLost {
		t.Fatalf("after the drop: %v", err)
	}
	if !s.Lost() {
		t.Fatal("session not marked lost")
	}
	if _, err := s.Shares(ctx); KindOf(err) != KindSessionLost {
		t.Fatalf("still usable: %v", err)
	}
}

// No credential reaches an error or a log line, whatever the outcome: every
// error from every login path is logged the ways ferry might log it, and
// neither the password nor the user name may appear.
func TestCredentialsNeverInErrorsOrLogs(t *testing.T) {
	sambaServer(t)
	const user = "alex"
	sentinels := []string{"S3ntinel-Pa55word-7f1c", alexPassword}
	var out bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	record := func(err error) {
		if err == nil {
			return
		}
		log.Error("login failed", "err", err, "kind", KindOf(err).String())
		fmt.Fprintf(&out, "%v|%+v|%#v|%s\n", err, err, err, errors.Unwrap(err))
	}

	// Wrong password, guest mapping, unreachable server.
	for _, c := range []struct{ server, user, pw string }{
		{sambaServer(t), user, sentinels[0]},
		{sambaServer(t), "nosuchuser-" + sentinels[0], sentinels[0]},
		{"127.0.0.1:1", user, sentinels[0]},
	} {
		_, err := Login(context.Background(), LoginOptions{Server: c.server, Timeout: 3 * time.Second}, Credentials{User: c.user, Password: c.pw})
		record(err)
	}
	// A good login, then errors on its session.
	s := mustLogin(t, user, alexPassword)
	_, err := s.ReadDir(context.Background(), "sam", "", 10)
	record(err)
	_ = s.conn.Close()
	_, err = s.Shares(context.Background())
	record(err)

	text := out.String()
	if text == "" {
		t.Fatal("nothing recorded")
	}
	for _, secret := range append(sentinels, "nosuchuser", `"alex"`) {
		if strings.Contains(text, secret) {
			t.Errorf("%q appears in the output:\n%s", secret, text)
		}
	}
}

func BenchmarkStream(b *testing.B) {
	s := mustLogin(b, "alex", alexPassword)
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		f, err := s.Open(ctx, "alex", "big.bin")
		if err != nil {
			b.Fatal(err)
		}
		// io.Copy takes File.WriteTo; io.Discard's own ReadFrom, with its
		// small reads, would measure that instead.
		n, err := io.Copy(io.Discard, f)
		_ = f.Close()
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(n)
	}
}
