package api

import (
	"bufio"
	"encoding/json"
	"testing"

	v1 "github.com/denisneuling/ferry/api/v1"
)

func TestUsageAPI(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	res := h.do(t, "GET", "/api/v1/shares/alex/usage?path=Search", nil, nil, c)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != ndjson {
		t.Fatalf("usage: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	var end *v1.Usage
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		var line struct {
			Progress *v1.Usage `json:"progress"`
			End      *v1.Usage `json:"end"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil || (line.Progress == nil && line.End == nil) {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		if line.End != nil {
			end = line.End
		}
	}
	if end == nil || end.Files != 5 || end.Bytes != 9 || end.Skipped != 1 || end.Truncated {
		t.Fatalf("end %+v", end)
	}
	for url, want := range map[string]int{
		"/api/v1/shares/sam/usage":                                   403,
		"/api/v1/shares/alex/usage?path=nowhere":                     404,
		"/api/v1/shares/alex/usage?path=Search&at=" + q("yesterday"): 400,
	} {
		if res := h.do(t, "GET", url, nil, nil, c); res.StatusCode != want {
			t.Errorf("%s: %d, want %d", url, res.StatusCode, want)
		}
	}
}
