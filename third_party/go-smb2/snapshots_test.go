package smb2

import (
	"encoding/binary"
	"reflect"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudsoda/go-smb2/internal/smb2"
)

func snapshotArray(tokens ...string) []byte {
	var u []uint16
	for _, t := range tokens {
		u = append(append(u, utf16.Encode([]rune(t))...), 0)
	}
	u = append(u, 0)
	out := make([]byte, 12+2*len(u))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(tokens)))
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(tokens)))
	binary.LittleEndian.PutUint32(out[8:12], uint32(2*len(u)))
	for i, c := range u {
		binary.LittleEndian.PutUint16(out[12+2*i:], c)
	}
	return out
}

func TestParseSnapshotArray(t *testing.T) {
	want := []string{"@GMT-2026.11.23-15.00.09", "@GMT-2026.11.23-14.00.09"}
	got, ok := parseSnapshotArray(snapshotArray(want...))
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, %v", got, ok)
	}
	if got, ok := parseSnapshotArray(snapshotArray()); !ok || len(got) != 0 {
		t.Fatalf("an empty array: %q, %v", got, ok)
	}
	// A size beyond the bytes sent, and too short a response, are refused.
	b := snapshotArray(want...)
	binary.LittleEndian.PutUint32(b[8:12], uint32(len(b)))
	if _, ok := parseSnapshotArray(b); ok {
		t.Fatal("a size beyond the response was read")
	}
	if _, ok := parseSnapshotArray(b[:8]); ok {
		t.Fatal("a short response was read")
	}
	// No more tokens than the server says it returned.
	b = snapshotArray(want...)
	binary.LittleEndian.PutUint32(b[4:8], 1)
	if got, _ := parseSnapshotArray(b); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("got %q", got)
	}
}

func TestTimewarp(t *testing.T) {
	at := time.Date(2026, 11, 23, 15, 0, 9, 0, time.UTC)
	p := make([]byte, timewarp(at).Size())
	timewarp(at).Encode(p)
	if string(p[16:20]) != "TWrp" || binary.LittleEndian.Uint32(p[12:16]) != 8 {
		t.Fatalf("not a TWrp context: % x", p)
	}
	// 2026-11-23 15:00:09 UTC in 100 ns since 1601.
	if got := binary.LittleEndian.Uint64(p[24:32]); got != 134399196090000000 {
		t.Fatalf("FILETIME %d", got)
	}
}

// The create contexts' length counts from the first context: a name whose
// length is no multiple of 8 is followed by padding that is not part of it.
func TestCreateContextsLength(t *testing.T) {
	for _, name := range []string{"Sub", `Docs\letter.txt`, `Docs\new.txt`, ""} {
		req := &smb2.CreateRequest{Name: name, Contexts: []smb2.Encoder{timewarp(time.Unix(0, 0))}}
		pkt := make([]byte, req.Size())
		req.Encode(pkt)
		r := smb2.CreateRequestDecoder(pkt[64:])
		if r.CreateContextsLength() != 32 {
			t.Errorf("%q: contexts length %d, want 32", name, r.CreateContextsLength())
		}
		if off := r.CreateContextsOffset(); off%8 != 0 || string(pkt[off+16:off+20]) != "TWrp" {
			t.Errorf("%q: the context is not at offset %d", name, off)
		}
	}
}
