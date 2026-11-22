package api

import (
	"context"
	"math"
	"net/http"
	"time"

	v1 "github.com/metapatrol/ferry/api/v1"
	"github.com/metapatrol/ferry/internal/meta"
	"github.com/metapatrol/ferry/internal/smb"
)

// metaConcurrency is how many files have their metadata read at once, each
// at most meta.Budget bytes: the memory it takes stays small and fixed.
const metaConcurrency = 4

// metaTimeout bounds one reading, waiting for a slot included: details are
// a courtesy, and a slow share should not hold them up for long.
const metaTimeout = 15 * time.Second

// GetMetadata reads what a file says about itself from its own bytes,
// within meta.Budget. Nothing is stored, and nothing of it — values,
// names, paths — goes to the log, a metric or a trace.
func (a *API) GetMetadata(w http.ResponseWriter, r *http.Request, share v1.ShareName, params v1.GetMetadataParams) {
	conn, ctx, done, ok := a.onShare(w, r, share, smb.PoolBackground)
	if !ok {
		return
	}
	defer done()
	p, ok := cleanParam(w, params.Path)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, metaTimeout)
	defer cancel()
	select {
	case a.metaSlots <- struct{}{}:
		defer func() { <-a.metaSlots }()
	case <-ctx.Done():
		probUnreachable.write(w)
		return
	}
	f, again, err := a.openDerived(ctx, r, conn, share, p)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer again()
	defer func() { _ = f.Close() }()

	tag := etag(f.Size, f.ModTime)
	tag = tag[:len(tag)-1] + `-meta"`
	h := w.Header()
	h.Set("ETag", tag)
	cacheDerived(h, params.V, f.Size, f.ModTime)
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, metadata(meta.Read(f, f.Size, p)))
}

// metadata turns what meta read into the API's shape, leaving out what the
// file did not say.
func metadata(m meta.Info) v1.FileMetadata {
	str := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	num := func(n int) *int {
		if n <= 0 {
			return nil
		}
		return &n
	}
	flt := func(f float64) *float64 {
		if f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) {
			return nil
		}
		return &f
	}
	out := v1.FileMetadata{
		Kind:        v1.FileMetadataKind(m.Kind),
		Format:      str(m.Format),
		Width:       num(m.Width),
		Height:      num(m.Height),
		Taken:       str(m.Taken),
		Camera:      str(m.Camera),
		Lens:        str(m.Lens),
		FNumber:     flt(m.FNumber),
		Exposure:    str(m.Exposure),
		Iso:         num(m.ISO),
		FocalLength: flt(m.FocalLength),
		Orientation: num(m.Orientation),
		Pages:       num(m.Pages),
		Title:       str(m.Title),
		Author:      str(m.Author),
		Album:       str(m.Album),
		Producer:    str(m.Producer),
		Codec:       str(m.Codec),
		Bitrate:     num(m.Bitrate),
		SampleRate:  num(m.SampleRate),
		Channels:    num(m.Channels),
	}
	if m.Duration > 0 {
		d := math.Round(m.Duration.Seconds()*1000) / 1000
		out.DurationSeconds = &d
	}
	if m.Location != nil {
		out.Location = &struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		}{m.Location.Latitude, m.Location.Longitude}
	}
	if m.Encoding != "" {
		enc := v1.FileMetadataEncoding(m.Encoding)
		out.Encoding = &enc
	}
	if m.Kind == meta.KindText {
		lines := m.Lines
		out.Lines = &lines
		if m.LinesAtLeast {
			t := true
			out.LinesAtLeast = &t
		}
		if m.LineEndings != "" {
			le := v1.FileMetadataLineEndings(m.LineEndings)
			out.LineEndings = &le
		}
	}
	return out
}
