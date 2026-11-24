package api

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
)

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(body))
	}
	_ = w.Close()
	return b.Bytes()
}

func TestUnpackArchive(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	stamp := time.Now().Format("150405.000")
	put := func(name string, z []byte) {
		t.Helper()
		if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(name), bytes.NewReader(z), ferryReq, c); res.StatusCode != 201 {
			t.Fatalf("upload %s: %d", name, res.StatusCode)
		}
		t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })
	}
	unpack := func(from, to string) (int, *v1.CopyEnd) {
		t.Helper()
		body, _ := json.Marshal(v1.UnpackRequest{From: from, To: to})
		res := h.do(t, "POST", "/api/v1/shares/alex/unpack", bytes.NewReader(body), ferryReq, c)
		if res.StatusCode != 200 {
			return res.StatusCode, nil
		}
		var end *v1.CopyEnd
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), `{"end"`) {
				end = &v1.CopyEnd{}
				_ = json.Unmarshal(sc.Bytes(), end)
			} else if strings.HasPrefix(sc.Text(), `{"problem"`) {
				t.Fatalf("problem: %s", sc.Text())
			}
		}
		return 200, end
	}

	big := strings.Repeat("ferry ", 300000) // more than one block
	good := "urlaub-" + stamp + ".zip"
	put(good, zipBytes(t, map[string]string{"Tag 1/IMG_1.jpg": "eins", "Tag 2/Notiz.txt": big, "README.md": "# Urlaub"}))
	to := "urlaub-" + stamp
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(to), nil, ferryReq, c) })
	status, end := unpack(good, to)
	if status != 200 || end == nil || end.End.Entry.Name != to || end.End.Progress.Bytes != int64(len(big)+12) {
		t.Fatalf("unpack: %d %+v", status, end)
	}
	got := h.do(t, "GET", "/api/v1/shares/alex/content?path="+q(to+"/Tag 2/Notiz.txt"), nil, nil, c)
	var b bytes.Buffer
	_, _ = b.ReadFrom(got.Body)
	if b.String() != big {
		t.Fatalf("Notiz.txt: %d bytes", b.Len())
	}
	// The folder exists now.
	if status, _ := unpack(good, to); status != 409 {
		t.Fatalf("into an existing folder: %d", status)
	}
	for name, c := range map[string]struct {
		files map[string]string
		want  int
	}{
		"up":        {map[string]string{"../escape.txt": "x"}, 422},
		"forbidden": {map[string]string{"a?.txt": "x"}, 422},
		"case":      {map[string]string{"Readme.md": "x", "README.md": "y"}, 409},
	} {
		z := name + "-" + stamp + ".zip"
		put(z, zipBytes(t, c.files))
		if status, _ := unpack(z, name+"-"+stamp); status != c.want {
			t.Errorf("%s: %d, want %d", name, status, c.want)
		}
	}
	if status, _ := unpack("hello.txt", "x-"+stamp); status != 415 {
		t.Errorf("not a ZIP: %d", status)
	}
}

// Packing onto the share, and unpacking what was packed: the same files.
func TestSaveArchive(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	stamp := time.Now().Format("150405.000")
	dir := "pack-" + stamp
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir), nil, ferryReq, c)
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c) })
	h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(dir+"/Fotos"), nil, ferryReq, c)
	big := strings.Repeat("0123456789", 250000)
	for name, body := range map[string]string{"Fotos/a.jpg": big, "notiz.txt": "hallo"} {
		if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(dir+"/"+name), strings.NewReader(body), ferryReq, c); res.StatusCode != 201 {
			t.Fatalf("upload %s: %d", name, res.StatusCode)
		}
	}
	save := func(paths []string, to string) (int, string) {
		body, _ := json.Marshal(v1.SaveArchiveRequest{Paths: paths, To: to})
		res := h.do(t, "POST", "/api/v1/shares/alex/archive-to-share", bytes.NewReader(body), ferryReq, c)
		var b bytes.Buffer
		_, _ = b.ReadFrom(res.Body)
		return res.StatusCode, b.String()
	}
	status, stream := save([]string{dir + "/Fotos", dir + "/notiz.txt"}, dir+"/alles.zip")
	if status != 200 || !strings.Contains(stream, `{"end"`) {
		t.Fatalf("save: %d %s", status, stream)
	}
	zl := decode[v1.ZipList](t, h.do(t, "GET", "/api/v1/shares/alex/zip?path="+q(dir+"/alles.zip"), nil, nil, c))
	var names []string
	for _, e := range zl.Entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, "|") != "Fotos/|Fotos/a.jpg|notiz.txt" {
		t.Fatalf("archive holds %q", names)
	}
	if status, _ := save([]string{dir + "/notiz.txt"}, dir+"/alles.zip"); status != 409 {
		t.Fatalf("over an existing file: %d", status)
	}
	if status, _ := save([]string{dir + "/Fotos"}, dir+"/Fotos/selbst.zip"); status != 400 {
		t.Fatalf("into itself: %d", status)
	}
	// And back: unpacked, the same bytes.
	body, _ := json.Marshal(v1.UnpackRequest{From: dir + "/alles.zip", To: dir + "/wieder"})
	if res := h.do(t, "POST", "/api/v1/shares/alex/unpack", bytes.NewReader(body), ferryReq, c); res.StatusCode != 200 {
		t.Fatalf("unpack: %d", res.StatusCode)
	} else {
		var b bytes.Buffer
		_, _ = b.ReadFrom(res.Body)
	}
	got := h.do(t, "GET", "/api/v1/shares/alex/content?path="+q(dir+"/wieder/Fotos/a.jpg"), nil, nil, c)
	var b bytes.Buffer
	_, _ = b.ReadFrom(got.Body)
	if b.String() != big {
		t.Fatalf("round trip: %d bytes", b.Len())
	}
}
