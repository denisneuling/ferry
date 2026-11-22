// Package heif carries libheif and libde265, compiled to WebAssembly, for
// ferry's HEIC thumbnails (internal/preview). The module is the one
// gen2brain/heic v0.4.9 built; README.md says what it is made of, under
// which licences, how to build it and how to run ferry with another.
package heif

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
)

//go:embed heif.wasm.gz
var gzipped []byte

// Module returns the module's bytes.
func Module() ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(gzipped))
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}
