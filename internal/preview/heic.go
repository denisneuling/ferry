package preview

// The host side of the HEIF module follows gen2brain/heic v0.4.9's
// decode.go (MIT, third_party/heif/LICENSE.gen2brain), which built the
// module: the exports malloc, free and decode, and how decode lays out the
// picture it writes. What differs is everything around a call: a runtime
// with a memory limit that aborts on the job's context, no file system, a
// new instance for every call, and the module's bytes taken from
// third_party/heif or the operator's file.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/metapatrol/ferry/internal/thumb"
)

const (
	// HEIFMemory is a libheif instance's limit. A 12.8-megapixel grid
	// decodes within it; see docs/operations.md.
	HEIFMemory = 96 << 20
	// heifCompileCost is what compiling the module took at its peak
	// (50 MB), with a margin.
	heifCompileCost = 80 << 20
	// thumbItemBox is the largest thumbnail made from a photo's embedded
	// thumbnail item; larger ones decode the photo itself.
	thumbItemBox = 320
	// heifThumbMax bounds a thumbnail item's bytes.
	heifThumbMax = 4 << 20
)

// ErrTooLarge is a photo beyond the caller's bounds, in bytes or pixels.
var ErrTooLarge = errors.New("preview: the file is too large")

// heifRuntime is a wazero runtime with the module compiled.
type heifRuntime struct {
	rt wazero.Runtime
	cm wazero.CompiledModule
}

func (h *heifRuntime) Close() error { return h.rt.Close(context.Background()) }

// initHEIF compiles or loads the module under the memory limit; the runtime
// aborts a call when the context it was made under ends.
func initHEIF(module []byte, cache wazero.CompilationCache, limit int64) (res *heifRuntime, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("heif: %v", r)
		}
	}()
	ctx := context.Background()
	rc := wazero.NewRuntimeConfig().WithMemoryLimitPages(uint32(limit / 65536)).WithCloseOnContextDone(true)
	if cache != nil {
		rc = rc.WithCompilationCache(cache)
	}
	rt := wazero.NewRuntimeWithConfig(ctx, rc)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	cm, err := rt.CompileModule(ctx, module)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	return &heifRuntime{rt: rt, cm: cm}, nil
}

func moduleHash(module []byte) string {
	h := sha256.Sum256(module)
	return hex.EncodeToString(h[:])
}

// HEIC returns a JPEG of the HEIF photo r (size bytes) that fits a box×box
// square. Up to thumbItemBox it is made from the photo's own thumbnail item
// when it has one; otherwise from the photo, which must be within maxBytes
// and maxPixels (ErrTooLarge).
func (e *Engine) HEIC(ctx context.Context, r io.ReaderAt, size int64, box int, maxBytes int64, maxPixels int) ([]byte, error) {
	if e.heic == nil {
		return nil, ErrUnavailable
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var data []byte
	if box <= thumbItemBox {
		// A file the parser does not follow may still be one libheif reads:
		// then the photo itself.
		data = safeThumbnail(r, size)
	}
	if data == nil {
		if size > maxBytes {
			return nil, ErrTooLarge
		}
	}
	res, done, err := e.heic.acquire(ctx, e)
	if err != nil {
		return nil, err
	}
	defer done()
	cost := JobCost(e.o.HEIFMemory) + int64(len(data))
	if data == nil {
		cost += size
	}
	release, err := e.o.Budget(ctx, cost)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer release()
	if data == nil {
		data = make([]byte, size)
		if _, err := r.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}
	hr := res.(*heifRuntime)
	cfg, err := heifDecode(ctx, hr, data, true, 0)
	if err != nil {
		return nil, jobError(ctx, err)
	}
	if cfg.w <= 0 || cfg.h <= 0 || cfg.w > maxPixels/cfg.h {
		return nil, ErrTooLarge
	}
	out, err := heifDecode(ctx, hr, data, false, box)
	if err != nil {
		return nil, jobError(ctx, err)
	}
	return out.jpeg, nil
}

// decodedHook, when set (by tests), is told the size of every picture
// decoded in full.
var decodedHook func(w, h int)

// safeThumbnail is heifThumbnail with a parser's bug on a hostile file
// turned into "no thumbnail item": the parser runs in ferry's process, not
// in the module.
func safeThumbnail(r io.ReaderAt, size int64) (data []byte) {
	defer func() {
		if recover() != nil {
			data = nil
		}
	}()
	data, _ = heifThumbnail(r, size, heifThumbMax)
	return data
}

type heifResult struct {
	w, h int
	jpeg []byte
}

const (
	heifColorspaceYCbCr      = 0
	heifColorspaceRGB        = 1
	heifColorspaceMonochrome = 2
	heifChroma420            = 1
	heifChroma422            = 2
	heifChroma444            = 3
	alignSize                = 16
)

func alignm(a int) int { return (a + (alignSize - 1)) & (^(alignSize - 1)) }

// heifDecode runs the module once, in a new instance closed afterwards:
// the dimensions only, or the picture made into a box-sized JPEG while it
// is still in the instance's memory.
func heifDecode(ctx context.Context, hr *heifRuntime, data []byte, configOnly bool, box int) (res heifResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = heifResult{}, fmt.Errorf("heif: %v", r)
		}
	}()
	// An unnamed instance: several may run at once.
	mod, err := hr.rt.InstantiateModule(ctx, hr.cm, wazero.NewModuleConfig().WithName("").WithStdout(io.Discard).WithStderr(io.Discard))
	if err != nil {
		return res, err
	}
	defer func() { _ = mod.Close(context.Background()) }()
	alloc, free, decode := mod.ExportedFunction("malloc"), mod.ExportedFunction("free"), mod.ExportedFunction("decode")
	if alloc == nil || free == nil || decode == nil {
		return res, errors.New("heif: the module lacks malloc, free or decode")
	}
	mem := mod.Memory()
	call := func(f interface {
		Call(context.Context, ...uint64) ([]uint64, error)
	}, args ...uint64) (uint64, error) {
		r, err := f.Call(ctx, args...)
		if err != nil {
			return 0, err
		}
		if len(r) == 0 {
			return 0, errors.New("heif: no result")
		}
		return r[0], nil
	}

	in, err := call(alloc, uint64(len(data)))
	if err != nil || in == 0 {
		return res, fmt.Errorf("heif: alloc: %w", err)
	}
	if !mem.Write(uint32(in), data) {
		return res, errors.New("heif: write")
	}
	info, err := call(alloc, 4*5)
	if err != nil || info == 0 {
		return res, fmt.Errorf("heif: alloc: %w", err)
	}
	ok, err := call(decode, in, uint64(len(data)), 1, info, info+4, info+8, info+12, info+16, 0)
	if err != nil {
		return res, err
	}
	if ok == 0 {
		return res, errors.New("heif: not a picture libheif decodes")
	}
	u32 := func(off uint64) int {
		v, _ := mem.ReadUint32Le(uint32(off))
		return int(v)
	}
	w, h, colorspace, chroma, premultiplied := u32(info), u32(info+4), u32(info+8), u32(info+12), u32(info+16)
	res.w, res.h = w, h
	if configOnly {
		return res, nil
	}
	if decodedHook != nil {
		decodedHook(w, h)
	}

	rect := image.Rect(0, 0, w, h)
	var size, stride, i0, i1, i2, ys, cs int
	var ratio image.YCbCrSubsampleRatio
	switch colorspace {
	case heifColorspaceYCbCr:
		var cw, ch int
		switch chroma {
		case heifChroma420:
			ratio, cw, ch = image.YCbCrSubsampleRatio420, (w+1)/2, (h+1)/2
		case heifChroma422:
			ratio, cw, ch = image.YCbCrSubsampleRatio422, (w+1)/2, h
		case heifChroma444:
			ratio, cw, ch = image.YCbCrSubsampleRatio444, w, h
		default:
			return res, fmt.Errorf("heif: chroma %d", chroma)
		}
		ys, cs = alignm(w), alignm(cw)
		i0, i1, i2 = ys*h, ys*h+cs*ch, ys*h+2*cs*ch
		size = i2
	case heifColorspaceMonochrome:
		stride = alignm(w)
		size = h * stride
	case heifColorspaceRGB:
		stride = alignm(w * 4)
		size = h * stride
	default:
		return res, fmt.Errorf("heif: colour space %d", colorspace)
	}
	out, err := call(alloc, uint64(size))
	if err != nil || out == 0 {
		return res, fmt.Errorf("heif: alloc %d: %w", size, err)
	}
	ok, err = call(decode, in, uint64(len(data)), 0, info, info+4, info+8, info+12, info+16, out)
	if err != nil {
		return res, err
	}
	if ok == 0 {
		return res, errors.New("heif: decoding failed")
	}
	pix, okRead := mem.Read(uint32(out), uint32(size))
	if !okRead {
		return res, errors.New("heif: read")
	}
	var img image.Image
	switch colorspace {
	case heifColorspaceYCbCr:
		img = &image.YCbCr{Y: pix[:i0:i0], Cb: pix[i0:i1:i1], Cr: pix[i1:i2:i2], SubsampleRatio: ratio, YStride: ys, CStride: cs, Rect: rect}
	case heifColorspaceMonochrome:
		img = &image.Gray{Pix: pix, Stride: stride, Rect: rect}
	default:
		if premultiplied == 1 {
			img = &image.RGBA{Pix: pix, Stride: stride, Rect: rect}
		} else {
			img = &image.NRGBA{Pix: pix, Stride: stride, Rect: rect}
		}
	}
	// libheif has turned the picture as its properties say; the pixels live
	// in the instance until it is closed, so the JPEG is made now.
	res.jpeg, err = thumb.FromImage(img, 1, box)
	return res, err
}
