package smb

import (
	"context"
	"errors"
	"io"
	"sort"
	"testing"
	"time"
)

// The Versions share of hack/samba: snapshots as a ZFS NAS offers them
// through vfs_shadow_copy2 (setup.sh writes them).
var (
	at1300 = time.Date(2026, 11, 23, 13, 0, 9, 0, time.UTC)
	at1400 = time.Date(2026, 11, 23, 14, 0, 9, 0, time.UTC)
	atSub  = time.Date(2026, 11, 22, 0, 0, 1, 0, time.UTC)
)

func TestVersions(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	cases := []struct {
		path string
		want []time.Time
	}{
		// Two states before the one now, newest first.
		{"Docs/letter.txt", []time.Time{at1400, at1300}},
		// The same in both snapshots as now: nothing to go back to.
		{"Docs/stable.txt", nil},
		// New since the snapshots.
		{"Docs/new.txt", nil},
		// Deleted since: one state, though the path is gone.
		{"Docs/gone.txt", []time.Time{at1300}},
		// A nested dataset has snapshots of its own, and none of the share's.
		{"Sub/Inner/inner.txt", []time.Time{atSub}},
		// A folder is a version in each snapshot that holds it, even with
		// the same time in both (setup.sh) and as now.
		{"Docs", []time.Time{at1400, at1300}},
	}
	for _, c := range cases {
		vs, _, err := s.Versions(ctx, "Versions", c.path, MaxVersions)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		var got []time.Time
		for _, v := range vs {
			got = append(got, v.At)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: versions at %v, want %v", c.path, got, c.want)
			continue
		}
		for i := range got {
			if !got[i].Equal(c.want[i]) {
				t.Errorf("%s: versions at %v, want %v", c.path, got, c.want)
			}
		}
	}
	vs, _, err := s.Versions(ctx, "Versions", "Docs/letter.txt", MaxVersions)
	if err != nil || len(vs) != 2 || vs[0].Size != int64(len("v2 middle\n")) || vs[1].Size != int64(len("v1\n")) ||
		!vs[0].ModTime.Equal(time.Date(2026, 11, 23, 13, 30, 0, 0, time.UTC)) {
		t.Fatalf("letter.txt's versions: %+v, %v", vs, err)
	}
	// A share without snapshots offers none, and that is no error.
	if vs, _, err := s.Versions(ctx, "alex", "hello.txt", MaxVersions); err != nil || len(vs) != 0 {
		t.Fatalf("a share without snapshots: %+v, %v", vs, err)
	}
	// The bound: only the newest snapshots are looked at.
	if vs, truncated, err := s.Versions(ctx, "Versions", "Docs/letter.txt", 1); err != nil || !truncated || len(vs) != 1 || !vs[0].At.Equal(at1400) {
		t.Fatalf("bounded to one: %+v, %v, %v", vs, truncated, err)
	}
	if _, truncated, err := s.Versions(ctx, "Versions", "Docs/letter.txt", MaxVersions); err != nil || truncated {
		t.Fatalf("all looked at: truncated %v, %v", truncated, err)
	}
}

func TestOpenAt(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	read := func(p string, at time.Time) (string, error) {
		f, err := s.OpenAt(ctx, "Versions", p, at)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		b, err := io.ReadAll(f)
		return string(b), err
	}
	for _, c := range []struct {
		path string
		at   time.Time
		want string
	}{
		{"Docs/letter.txt", at1300, "v1\n"},
		{"Docs/letter.txt", at1400, "v2 middle\n"},
		{"Docs/gone.txt", at1300, "gone since\n"},
		{"Sub/Inner/inner.txt", atSub, "inner then\n"},
	} {
		if got, err := read(c.path, c.at); err != nil || got != c.want {
			t.Errorf("%s at %s: %q, %v", c.path, Token(c.at), got, err)
		}
	}
	// Not in that snapshot, a time that is no snapshot, the share's time on
	// the nested dataset: not found.
	for _, c := range []struct {
		path string
		at   time.Time
	}{
		{"Docs/new.txt", at1300},
		{"Docs/letter.txt", at1300.Add(time.Second)},
		{"Sub/Inner/inner.txt", at1300},
	} {
		if _, err := read(c.path, c.at); KindOf(err) != KindNotFound {
			t.Errorf("%s at %s: %v, want not found", c.path, Token(c.at), err)
		}
	}
	// A folder as it was, listed.
	d, err := s.OpenDirAt(ctx, "Versions", "Docs", 100, at1300)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for {
		batch, err := d.Next()
		for _, e := range batch {
			names = append(names, e.Name)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = d.Close()
	sort.Strings(names)
	if want := []string{"gone.txt", "letter.txt", "stable.txt"}; len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("Docs at 13:00:09: %v", names)
	}
	if e, err := s.StatAt(ctx, "Versions", "Docs/letter.txt", at1300); err != nil || e.Size != 3 || e.Dir {
		t.Fatalf("StatAt: %+v, %v", e, err)
	}
	// The snapshot directory itself is hidden from listings (veto files, as
	// ZFS hides it), and a token in the path is refused before the server.
	if _, err := s.Stat(ctx, "Versions", "@GMT-2026.11.23-13.00.09/Docs/letter.txt"); KindOf(err) != KindInvalidPath {
		t.Fatalf("a token in the path: %v", err)
	}
}

// Samba resolves the time in an open against the snapshot list it last
// enumerated on the connection. After enumerating another dataset, an open
// at this one's time fails on its own; openAt enumerates the path itself
// first and succeeds.
func TestOpenAtEnumeratesItsPath(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	c, err := s.start(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.end()
	m, err := s.mount(c.ctx, "Versions")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.snapshots(m, `Sub\Inner\inner.txt`); err != nil {
		t.Fatal(err)
	}
	if f, err := m.OpenAt(`Docs\letter.txt`, at1300); err == nil {
		_ = f.Close()
		t.Fatal("the server opened a time it had not enumerated: openAt's enumeration may be unneeded")
	}
	if _, err := s.snapshots(m, `Sub\Inner\inner.txt`); err != nil {
		t.Fatal(err)
	}
	f, err := s.openAt(m, `Docs\letter.txt`, at1300)
	if err != nil {
		t.Fatalf("openAt after another dataset's enumeration: %v", err)
	}
	_ = f.Close()
}

// Nothing writes through a snapshot token in a path: it is refused before
// the server, whose older versions would read it as a time.
func TestNoTokenInWrites(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	ctx := context.Background()
	if _, err := s.Upload(ctx, "Versions", "@GMT-2026.11.23-13.00.09/Docs/x.txt", nil, false, 1); KindOf(err) != KindInvalidPath {
		t.Fatalf("an upload into a snapshot: %v", err)
	}
}

// When the request's deadline passes between two snapshots, the versions
// found so far are the answer, marked truncated, and no error: the hook
// holds the first look until the deadline has passed, so no timing is left
// to luck.
func TestVersionsDeadline(t *testing.T) {
	s := mustLogin(t, "alex", alexPassword)
	statHook = func(ctx context.Context, n int) {
		if n == 1 {
			<-ctx.Done()
		}
	}
	t.Cleanup(func() { statHook = nil })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	vs, truncated, err := s.Versions(ctx, "Versions", "Docs/letter.txt", MaxVersions)
	if err != nil || !truncated || len(vs) != 1 || !vs[0].At.Equal(at1400) {
		t.Fatalf("after the deadline: %+v, truncated %v, %v", vs, truncated, err)
	}
	// A request that ended otherwise — the client left — is an error.
	ctx, cancel = context.WithCancel(context.Background())
	statHook = func(_ context.Context, n int) {
		if n == 1 {
			cancel()
		}
	}
	if _, _, err := s.Versions(ctx, "Versions", "Docs/letter.txt", MaxVersions); KindOf(err) != KindCanceled {
		t.Fatalf("a request that ended: %v", err)
	}
}
