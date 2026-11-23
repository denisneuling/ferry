package api

import (
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/denisneuling/ferry/api/v1"
)

func TestAtParam(t *testing.T) {
	ok := func(s string) bool {
		_, good := atParam(httptest.NewRecorder(), &s)
		return good
	}
	if at, good := atParam(httptest.NewRecorder(), nil); !good || !at.IsZero() {
		t.Fatal("no at is not the file as it is now")
	}
	if !ok("@GMT-2026.11.23-15.00.09") {
		t.Fatal("a token is refused")
	}
	for _, bad := range []v1.At{"", "@GMT-2026.11.23-15.00.9", "@GMT-2026.11.23-15.00.09/x", "now", "@gmt-2026.11.23-15.00.09"} {
		if ok(bad) {
			t.Errorf("%q is taken", bad)
		}
	}
}

// A version's thumbnail, details and download are kept and revalidated
// apart from the file as it is now and from the file at other snapshots.
func TestAtKeysAndTags(t *testing.T) {
	a := time.Date(2026, 11, 23, 13, 0, 9, 0, time.UTC)
	b := a.Add(time.Hour)
	variants := map[string]bool{}
	for _, v := range []string{atVariant("320", time.Time{}), atVariant("320", a), atVariant("320", b), atVariant("", a), atVariant("", time.Time{})} {
		if variants[v] {
			t.Fatalf("variant %q twice", v)
		}
		variants[v] = true
	}
	tags := map[string]bool{}
	for _, tag := range []string{atTag(`"12-34"`, time.Time{}), atTag(`"12-34"`, a), atTag(`"12-34"`, b)} {
		if tags[tag] || tag[0] != '"' || tag[len(tag)-1] != '"' {
			t.Fatalf("tag %s", tag)
		}
		tags[tag] = true
	}
}
