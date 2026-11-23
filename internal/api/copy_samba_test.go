package api

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	v1 "github.com/denisneuling/ferry/api/v1"
)

// copyLines posts a copy and reads its answer: the progress lines, and the
// last line, end or problem.
func copyLines(t *testing.T, h *harness, c *http.Cookie, share string, req v1.CopyRequest) (status int, progress []v1.CopyProgress, end *v1.CopyEnd, prob *v1.CopyProblem) {
	t.Helper()
	body, _ := json.Marshal(req)
	res := h.do(t, "POST", "/api/v1/shares/"+share+"/copy", bytes.NewReader(body), ferryReq, c)
	if res.StatusCode != 200 {
		return res.StatusCode, nil, nil, nil
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		var line map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		switch {
		case line["end"] != nil:
			end = &v1.CopyEnd{}
			_ = json.Unmarshal(sc.Bytes(), end)
		case line["problem"] != nil:
			prob = &v1.CopyProblem{}
			_ = json.Unmarshal(sc.Bytes(), prob)
		case line["progress"] != nil:
			var p v1.CopyProgress
			_ = json.Unmarshal(line["progress"], &p)
			progress = append(progress, p)
		}
	}
	return 200, progress, end, prob
}

func TestCopyAPI(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	dir := "copy-" + hex.EncodeToString(b)
	put := func(p, content string) {
		t.Helper()
		if res := h.do(t, "PUT", "/api/v1/shares/alex/content?path="+q(p), strings.NewReader(content), ferryReq, c); res.StatusCode != 201 {
			t.Fatalf("upload %s: %d", p, res.StatusCode)
		}
	}
	get := func(share, p string) string {
		t.Helper()
		res := h.do(t, "GET", "/api/v1/shares/"+share+"/content?path="+q(p), nil, nil, c)
		got, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			return ""
		}
		return string(got)
	}
	big := strings.Repeat("ferry ", 600000) // 3.6 MB: several chunks
	for _, d := range []string{dir, dir + "/src", dir + "/src/sub"} {
		if res := h.do(t, "POST", "/api/v1/shares/alex/folders?path="+q(d), nil, ferryReq, c); res.StatusCode != 201 {
			t.Fatalf("folder %s: %d", d, res.StatusCode)
		}
	}
	put(dir+"/src/a.txt", "a")
	put(dir+"/src/sub/b.txt", big)
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?recursive=true&path="+q(dir), nil, ferryReq, c) })

	// A file.
	status, prog, end, prob := copyLines(t, h, c, "alex", v1.CopyRequest{From: dir + "/src/sub/b.txt", To: dir + "/b-copy.txt"})
	if status != 200 || end == nil || prob != nil || end.End.Entry.Name != "b-copy.txt" || len(prog) == 0 || end.End.Progress.Bytes != int64(len(big)) {
		t.Fatalf("a file: %d %+v %+v", status, end, prob)
	}
	if get("alex", dir+"/b-copy.txt") != big {
		t.Fatal("the copy differs")
	}
	// A folder, with all in it, the totals told up front.
	status, prog, end, _ = copyLines(t, h, c, "alex", v1.CopyRequest{From: dir + "/src", To: dir + "/dst"})
	if status != 200 || end == nil || prog[0].TotalFiles != 4 || end.End.Progress.Files != 4 || end.End.Entry.Type != v1.EntryTypeFolder {
		t.Fatalf("a folder: %d %+v %+v", status, prog, end)
	}
	if get("alex", dir+"/dst/a.txt") != "a" || get("alex", dir+"/dst/sub/b.txt") != big {
		t.Fatal("the folder's copy differs")
	}

	// Refused before anything is copied: problem responses.
	for _, r := range []struct {
		req  v1.CopyRequest
		want int
		code string
	}{
		{v1.CopyRequest{From: dir + "/src", To: dir + "/src/sub/x"}, 400, "copy_into_itself"},
		{v1.CopyRequest{From: dir + "/src", To: dir + "/src"}, 400, "copy_into_itself"},
		{v1.CopyRequest{From: dir + "/src/a.txt", To: dir + "/b-copy.txt"}, 409, "exists"},
		{v1.CopyRequest{From: dir + "/src", To: dir + "/dst"}, 409, "exists"},
		{v1.CopyRequest{From: dir + "/src/a.txt", To: dir + "/dst"}, 409, "is_a_folder"},
		{v1.CopyRequest{From: dir + "/nothing", To: dir + "/x"}, 404, "not_found"},
	} {
		body, _ := json.Marshal(r.req)
		res := h.do(t, "POST", "/api/v1/shares/alex/copy", bytes.NewReader(body), ferryReq, c)
		if res.StatusCode != r.want || decode[v1.Problem](t, res).Code != r.code {
			t.Errorf("%+v: %d, want %d %s", r.req, res.StatusCode, r.want, r.code)
		}
	}
	// With overwrite, a file replaces a file.
	ow := true
	if status, _, end, _ := copyLines(t, h, c, "alex", v1.CopyRequest{From: dir + "/src/a.txt", To: dir + "/b-copy.txt", Overwrite: &ow}); status != 200 || end == nil || get("alex", dir+"/b-copy.txt") != "a" {
		t.Fatalf("overwrite: %d %+v", status, end)
	}

	// A previous version restored as a copy, and a deleted file's folder.
	at := "@GMT-2026.11.23-13.00.09"
	t.Cleanup(func() {
		h.do(t, "DELETE", "/api/v1/shares/Versions/entries?path="+q("Docs/letter (restored).txt"), nil, ferryReq, c)
		h.do(t, "DELETE", "/api/v1/shares/Versions/entries?recursive=true&path="+q("Docs restored"), nil, ferryReq, c)
	})
	if status, _, end, _ := copyLines(t, h, c, "Versions", v1.CopyRequest{From: "Docs/letter.txt", To: "Docs/letter (restored).txt", At: &at}); status != 200 || end == nil || get("Versions", "Docs/letter (restored).txt") != "v1\n" {
		t.Fatalf("restore a version: %d %+v", status, end)
	}
	if status, _, end, _ := copyLines(t, h, c, "Versions", v1.CopyRequest{From: "Docs", To: "Docs restored", At: &at}); status != 200 || end == nil || get("Versions", "Docs restored/gone.txt") != "gone since\n" {
		t.Fatalf("restore a folder: %d %+v", status, end)
	}
}
