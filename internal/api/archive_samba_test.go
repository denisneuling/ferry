package api

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/config"
)

func archiveRequest(t *testing.T, h *harness, c *http.Cookie, share string, paths ...string) (*http.Response, v1.ArchiveTicket) {
	t.Helper()
	body, _ := json.Marshal(v1.ArchiveRequest{Paths: paths})
	res := h.do(t, "POST", "/api/v1/shares/"+share+"/archive", bytes.NewReader(body), ferryReq, c)
	var tk v1.ArchiveTicket
	if res.StatusCode == 201 {
		tk = decode[v1.ArchiveTicket](t, res)
	}
	return res, tk
}

// TestArchive downloads a ZIP of a file and a folder from the test Samba and
// reads it back: the names under the folder they were chosen in, folders
// as entries, the bytes and times as they are on the share. A ticket is
// good once and for its session only.
func TestArchive(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "archive-" + hex.EncodeToString(b)
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c)
	})
	files := map[string]string{
		"notes.txt":              "a note",
		"Fotos/Strand.jpg":       strings.Repeat("jpeg", 300000),
		"Fotos/Übersicht ä.txt":  "umlauts in the name",
		"Fotos/Leer/.keep":       "",
		"not chosen/elsewhe.txt": "stays out",
	}
	for _, d := range []string{"Fotos", "Fotos/Leer", "not chosen"} {
		h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir+"/"+d), nil, ferryReq, c)
	}
	for name, content := range files {
		if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/"+name), strings.NewReader(content), ferryReq, c); res.StatusCode != 201 {
			t.Fatalf("upload %s: %d", name, res.StatusCode)
		}
	}

	res, tk := archiveRequest(t, h, c, "alex", dir+"/notes.txt", dir+"/Fotos")
	if res.StatusCode != 201 || tk.Name != dir+".zip" || tk.Entries != 6 || tk.Bytes != int64(6+1200000+19) {
		t.Fatalf("ticket: %d %+v", res.StatusCode, tk)
	}
	res = h.do(t, "GET", "/api/v1/shares/alex/archive/"+tk.Ticket, nil, nil, c)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/zip" || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %d %v", res.StatusCode, res.Header)
	}
	data, _ := io.ReadAll(res.Body)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Method != zip.Store {
			t.Errorf("%s compressed", f.Name)
		}
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err) // a CRC mismatch shows here
		}
		if want := files[f.Name]; string(got) != want {
			t.Errorf("%s: %d bytes, want %d", f.Name, len(got), len(want))
		}
	}
	sort.Strings(names)
	want := []string{"Fotos/", "Fotos/Leer/", "Fotos/Leer/.keep", "Fotos/Strand.jpg", "Fotos/Übersicht ä.txt", "notes.txt"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("names %q", names)
	}
	list := decode[v1.EntryList](t, h.do(t, "GET", "/api/v1/shares/alex/entries?path="+q(dir), nil, nil, c))
	for _, e := range list.Entries {
		if e.Name != "notes.txt" {
			continue
		}
		for _, f := range zr.File {
			if f.Name == "notes.txt" && !f.Modified.Truncate(2e9).Equal(e.Modified.Truncate(2e9)) {
				t.Errorf("time %v, on the share %v", f.Modified, e.Modified)
			}
		}
	}

	// Once only, and only for its session.
	if res := h.do(t, "GET", "/api/v1/shares/alex/archive/"+tk.Ticket, nil, nil, c); res.StatusCode != 404 {
		t.Fatalf("a ticket used twice: %d", res.StatusCode)
	}
	_, tk = archiveRequest(t, h, c, "alex", dir+"/notes.txt")
	other := h.login(t, "alex", alexPassword)
	if res := h.do(t, "GET", "/api/v1/shares/alex/archive/"+tk.Ticket, nil, nil, other); res.StatusCode != 404 {
		t.Fatalf("another session's ticket: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", "/api/v1/shares/alex/archive/"+tk.Ticket, nil, nil, nil); res.StatusCode != 401 {
		t.Fatalf("without a session: %d", res.StatusCode)
	}

	// What is refused before anything is downloaded.
	for _, c2 := range []struct {
		share  string
		paths  []string
		status int
		code   string
	}{
		{"alex", []string{dir + "/notes.txt", dir + "/Fotos/Strand.jpg"}, 400, "invalid_request"}, // two folders
		{"alex", []string{dir + "/nope"}, 404, "not_found"},
		{"alex", []string{"../x"}, 400, "invalid_path"},
		{"alex", []string{""}, 400, "invalid_path"},
		{"sam", []string{"private.txt"}, 403, "access_denied"},
	} {
		res, _ := archiveRequest(t, h, other, c2.share, c2.paths...)
		if res.StatusCode != c2.status || problemCode(t, res) != c2.code {
			t.Errorf("%s %v: %d", c2.share, c2.paths, res.StatusCode)
		}
	}
}

// A tree beyond FERRY_MAX_ARCHIVE is refused whole, saying the bound.
func TestArchiveLimit(t *testing.T) {
	h := sambaHarnessWith(t, func(c *config.Config) { c.MaxArchive = 100 })
	c := h.login(t, "alex", alexPassword)
	res, _ := archiveRequest(t, h, c, "alex", "Many")
	if res.StatusCode != 413 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if p := decode[v1.Problem](t, res); p.Limit == nil || *p.Limit != 100 {
		t.Fatalf("problem %+v", p)
	}
}

// A file that fails once the archive has begun breaks the download off: the
// client gets an error, not an archive that looks complete.
func TestArchiveBrokenOff(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "archive-broken-" + hex.EncodeToString(b)
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c)
	})
	for _, n := range []string{"a.txt", "b.txt"} {
		h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/"+n), strings.NewReader(strings.Repeat(n, 1000)), ferryReq, c)
	}
	_, tk := archiveRequest(t, h, c, "alex", dir+"/a.txt", dir+"/b.txt")
	// b.txt goes after it was listed.
	h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(dir+"/b.txt"), nil, ferryReq, c)

	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/shares/alex/archive/"+tk.Ticket, nil)
	req.AddCookie(c)
	res, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err == nil {
		if _, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data))); zerr == nil {
			t.Fatal("a complete-looking archive with a file missing")
		}
	}
	if res := h.do(t, "GET", "/api/v1/me", nil, nil, c); res.StatusCode != 200 {
		t.Fatalf("the session did not survive: %d", res.StatusCode)
	}
}
