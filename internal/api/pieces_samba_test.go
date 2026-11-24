package api

import (
	"bytes"
	"strings"
	"testing"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
)

// An upload in pieces through the API: begun, a piece, a piece at the wrong
// place, asked, finished.
func TestUploadInPieces(t *testing.T) {
	h := sambaHarness(t)
	c := h.login(t, "alex", alexPassword)
	name := "pieces-" + time.Now().Format("150405.000") + ".bin"
	base := "/api/v1/shares/alex/uploads"
	t.Cleanup(func() { h.do(t, "DELETE", "/api/v1/shares/alex/entries?path="+q(name), nil, ferryReq, c) })

	res := h.do(t, "POST", base+"?path="+q(name), nil, ferryReq, c)
	if res.StatusCode != 201 {
		t.Fatalf("begin: %d", res.StatusCode)
	}
	id := decode[v1.UploadBegun](t, res).Id
	one := "/" + id + "?path=" + q(name)
	if res := h.do(t, "PUT", base+one+"&offset=0", strings.NewReader("hello "), ferryReq, c); res.StatusCode != 200 || decode[v1.UploadState](t, res).Size != 6 {
		t.Fatalf("first piece: %d", res.StatusCode)
	}
	if res := h.do(t, "PUT", base+one+"&offset=0", strings.NewReader("hello "), ferryReq, c); res.StatusCode != 409 {
		t.Fatalf("a piece again: %d", res.StatusCode)
	}
	if st := decode[v1.UploadState](t, h.do(t, "GET", base+one, nil, nil, c)); st.Size != 6 {
		t.Fatalf("state %+v", st)
	}
	if res := h.do(t, "PUT", base+one+"&offset=6", bytes.NewReader([]byte("world")), ferryReq, c); res.StatusCode != 200 {
		t.Fatalf("second piece: %d", res.StatusCode)
	}
	if res := h.do(t, "POST", base+"/"+id+"/finish?size=11&path="+q(name), nil, ferryReq, c); res.StatusCode != 201 {
		t.Fatalf("finish: %d", res.StatusCode)
	}
	got := h.do(t, "GET", "/api/v1/shares/alex/content?path="+q(name), nil, nil, c)
	var b bytes.Buffer
	_, _ = b.ReadFrom(got.Body)
	if b.String() != "hello world" {
		t.Fatalf("content %q", b.String())
	}
	// Without the header, and with an id that is not one: refused.
	if res := h.do(t, "POST", base+"?path="+q(name+"2"), nil, nil, c); res.StatusCode != 400 {
		t.Fatalf("without X-Ferry-Request: %d", res.StatusCode)
	}
	if res := h.do(t, "GET", base+"/..%2F..%2Fx?path="+q(name), nil, nil, c); res.StatusCode != 400 && res.StatusCode != 404 {
		t.Fatalf("a made-up id: %d", res.StatusCode)
	}
}
