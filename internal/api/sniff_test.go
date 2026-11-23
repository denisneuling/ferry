package api

import "testing"

// FuzzIsHEIC feeds the ftyp check that decides, on the first bytes of any
// file, whether it goes to the HEIC decoder: it never reads beyond them,
// and says yes only to an ftyp box.
func FuzzIsHEIC(f *testing.F) {
	f.Add([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"))
	f.Add([]byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf"))
	f.Add([]byte("\xff\xff\xff\xffftypmif1\x00\x00\x00\x00hevc"))
	f.Fuzz(func(t *testing.T, h []byte) {
		if isHEIC(h) && string(h[4:8]) != "ftyp" {
			t.Fatalf("%q is no ftyp box", h)
		}
	})
}
