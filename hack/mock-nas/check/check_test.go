// Package check verifies that the mock NAS (hack/mock-nas) is what its
// README promises: two people kept apart, shared shares that both can write,
// a read-only one, no guests. `make dev-nas-check` runs it against a running
// mock; without FERRY_TEST_MOCK_NAS it skips.
package check

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/metapatrol/ferry/internal/smb"
)

func login(t *testing.T, user, password string) (*smb.Session, error) {
	t.Helper()
	addr := os.Getenv("FERRY_TEST_MOCK_NAS")
	if addr == "" {
		t.Skip("FERRY_TEST_MOCK_NAS is not set (make dev-nas)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return smb.Login(ctx, smb.LoginOptions{Server: addr}, smb.Credentials{User: user, Password: password})
}

func must(t *testing.T, user, password string) *smb.Session {
	t.Helper()
	s, err := login(t, user, password)
	if err != nil {
		t.Fatalf("login as %s: %v", user, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestNoGuests(t *testing.T) {
	for _, c := range [][2]string{{"alex", "wrong"}, {"nobody-here", "x"}} {
		if _, err := login(t, c[0], c[1]); smb.KindOf(err) != smb.KindAuth {
			t.Errorf("%s: %v, want auth", c[0], err)
		}
	}
}

func TestPeopleKeptApart(t *testing.T) {
	ctx := context.Background()
	alex, sam := must(t, "alex", "alex-nas-demo"), must(t, "sam", "sam-nas-demo")
	if _, err := alex.ReadDir(ctx, "sam", "", 100); smb.KindOf(err) != smb.KindAccessDenied {
		t.Errorf("alex in sam's share: %v", err)
	}
	if _, err := sam.ReadDir(ctx, "alex", "", 100); smb.KindOf(err) != smb.KindAccessDenied {
		t.Errorf("sam in alex's share: %v", err)
	}
	for _, s := range []*smb.Session{alex, sam} {
		for _, share := range []string{"Family", "Public", "Scans"} {
			if _, err := s.ReadDir(ctx, share, "", 1000); err != nil {
				t.Errorf("%s: %v", share, err)
			}
		}
	}
}

func TestSharedWritesAndReadOnly(t *testing.T) {
	ctx := context.Background()
	alex, sam := must(t, "alex", "alex-nas-demo"), must(t, "sam", "sam-nas-demo")
	name := "check-" + time.Now().Format("150405.000") + ".txt"
	if _, err := alex.Upload(ctx, "Family", name, strings.NewReader("from alex"), false, 0); err != nil {
		t.Fatalf("alex writes to Family: %v", err)
	}
	// force group and create mask make it sam's to change too.
	if _, err := sam.Upload(ctx, "Family", name, strings.NewReader("from sam"), true, 0); err != nil {
		t.Fatalf("sam replaces alex's file in Family: %v", err)
	}
	if err := sam.Remove(ctx, "Family", name, false, 10); err != nil {
		t.Fatalf("sam deletes it: %v", err)
	}
	if _, err := alex.Upload(ctx, "Public", name, strings.NewReader("x"), false, 0); smb.KindOf(err) != smb.KindAccessDenied {
		t.Fatalf("a write to Public: %v", err)
	}
}

func TestSampleFiles(t *testing.T) {
	ctx := context.Background()
	alex := must(t, "alex", "alex-nas-demo")
	for _, c := range []struct {
		dir  string
		want string
	}{
		{"Photos", "Panorama Berge 🏔️.jpg"},
		{"Videos", "Urlaub am Meer 🌊 2025.mp4"},
		{"Documents/Steuern 2025", "Übersicht.md"},
		{"Documents/Steuern 2025/Belege/Q1/Januar", "Rechnung Fahrrad.pdf"},
	} {
		entries, err := alex.ReadDir(ctx, "alex", c.dir, 1000)
		if err != nil {
			t.Fatalf("%s: %v", c.dir, err)
		}
		found := false
		for _, e := range entries {
			found = found || e.Name == c.want
		}
		if !found {
			t.Errorf("%s lacks %s", c.dir, c.want)
		}
	}
	entries, err := alex.ReadDir(ctx, "alex", "Archiv/Briefe", 1000)
	if err != nil || len(entries) != 250 {
		t.Errorf("Archiv/Briefe: %d entries, %v", len(entries), err)
	}
}
