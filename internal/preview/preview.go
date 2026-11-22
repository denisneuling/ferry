// Package preview makes thumbnails of formats Go cannot decode — a PDF's
// first page so far — by running C libraries compiled to WebAssembly in
// wazero, inside ferry's process and yet apart from it.
//
// A module sees nothing but the bytes it is handed: no file system (the
// libraries' defaults would mount the host's root), no network, no clock
// worth anything, and a fixed amount of memory. Each job gets a new
// instance, which is killed afterwards — after a malformed file too — and
// never reused. A job that runs past its context is aborted inside the
// module (WithCloseOnContextDone).
//
// Compiling a module to machine code is the expensive part: pdfium's takes
// about 1.8 s and 340 MB at its peak, more than a container sized for ferry
// can spare next to its thumbnails. The image therefore ships the compiled
// code (`ferry precompile`, run when the image is built); a runtime is made
// from it when the first job comes and closed again after a while without
// one. Without that code, a compile is a cost like any other and is taken
// from the budget the caller passes, or the format is off for the process.
// docs/operations.md has the measurements.
package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	"github.com/metapatrol/ferry/internal/thumb"
	"github.com/metapatrol/ferry/third_party/heif"
)

var (
	// ErrUnavailable is a format that is off, or that this process could not
	// prepare (no compiled code and no budget for compiling it).
	ErrUnavailable = errors.New("preview: not available")
	// ErrUnsupported is a file the library could not read.
	ErrUnsupported = errors.New("preview: cannot read the file")
	// ErrBlank is a rendering of one colour. pdfium answers a page it ran
	// out of memory on with a white one instead of an error, so such a
	// result is not trusted: it gets no thumbnail and is not cached.
	ErrBlank = errors.New("preview: the result is blank")
)

const (
	// PDFMemory is a pdfium instance's memory limit: a page with a
	// 12-megapixel scan took 48 MiB.
	PDFMemory = 96 << 20
	// compileCost is what compiling pdfium took at its peak (340 MB),
	// with a margin.
	compileCost = 400 << 20
	// DefaultIdle is how long a runtime is kept without a job.
	DefaultIdle = 5 * time.Minute
)

// JobCost is what a job is charged against the caller's budget: wazero
// keeps an instance's memory in a Go slice and copies it as it grows, so
// a job holds up to about twice its limit, and the rest — the result, the
// bitmap — fits in 16 MB.
func JobCost(limit int64) int64 { return 2*limit + 16<<20 }

// Budget takes n bytes from the caller's memory budget, waiting under ctx,
// and returns the function that gives them back. It fails at once when n
// is more than the whole budget.
type Budget func(ctx context.Context, n int64) (release func(), err error)

// Options configure an Engine.
type Options struct {
	// PDF turns PDF previews on.
	PDF bool
	// PDFMemory is a pdfium instance's limit; 0 is PDFMemory.
	PDFMemory int64
	// HEIC turns HEIC previews on; HEIFModule replaces the module ferry
	// carries (FERRY_HEIF_WASM); HEIFMemory is an instance's limit.
	HEIC       bool
	HEIFModule []byte
	HEIFMemory int64
	// CodeDir holds compiled code made by Precompile; it may be read-only.
	// Empty or not matching this build: modules are compiled when needed.
	CodeDir string
	// Budget is charged for every job and every compile; BudgetSize is all
	// of it. A format whose job alone would not fit is off from the start.
	Budget     Budget
	BudgetSize int64
	// Concurrency is how many jobs run at once; at least 1.
	Concurrency int
	// Idle closes a runtime after this long without a job (DefaultIdle).
	Idle time.Duration
	Log  *slog.Logger
}

// Engine runs the jobs. Its methods are safe for concurrent use.
type Engine struct {
	o     Options
	slots chan struct{}
	pdf   *slot
	heic  *slot
}

// slot is one format's runtime: made with the first job, closed when idle.
type slot struct {
	name string // in the log: "pdf", "heic"
	// load compiles the module, or loads it from cache when given one.
	load func(cache wazero.CompilationCache) (closer, error)
	// compile is what compiling takes at its peak, charged to the budget.
	compile int64
	// key and want find the module in the compiled code's marker.
	key, want string

	mu    sync.Mutex
	res   closer
	cache wazero.CompilationCache
	off   error // why the format is off for this process
	jobs  int
	timer *time.Timer
}

type closer interface{ Close() error }

// New makes an Engine; nothing is compiled or loaded yet.
func New(o Options) *Engine {
	if o.Concurrency < 1 {
		o.Concurrency = 1
	}
	if o.Idle <= 0 {
		o.Idle = DefaultIdle
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.PDFMemory <= 0 {
		o.PDFMemory = PDFMemory
	}
	if o.HEIFMemory <= 0 {
		o.HEIFMemory = HEIFMemory
	}
	if o.Budget == nil {
		o.Budget = func(context.Context, int64) (func(), error) { return func() {}, nil }
	}
	e := &Engine{o: o, slots: make(chan struct{}, o.Concurrency)}
	if o.PDF {
		if o.BudgetSize > 0 && JobCost(o.PDFMemory) > o.BudgetSize {
			// Said once here rather than on every PDF; the interface does not
			// ask for what Formats leaves out.
			o.Log.Warn("pdf previews are off: a page needs more than the thumbnail budget",
				"needs", JobCost(o.PDFMemory), "budget", o.BudgetSize)
		} else {
			limit := o.PDFMemory
			e.pdf = &slot{
				name: "pdf", compile: compileCost, key: "pdf", want: current().Modules["pdf"],
				load: func(c wazero.CompilationCache) (closer, error) { return initPDF(c, limit) },
			}
		}
	}
	if o.HEIC {
		module := o.HEIFModule
		var err error
		if module == nil {
			module, err = heif.Module()
		}
		switch {
		case err != nil:
			o.Log.Warn("heic previews are off", "error", err.Error())
		case o.BudgetSize > 0 && JobCost(o.HEIFMemory) > o.BudgetSize:
			o.Log.Warn("heic previews are off: a photo needs more than the thumbnail budget",
				"needs", JobCost(o.HEIFMemory), "budget", o.BudgetSize)
		default:
			limit := o.HEIFMemory
			e.heic = &slot{
				name: "heic", compile: heifCompileCost, key: "heif", want: moduleHash(module),
				load: func(c wazero.CompilationCache) (closer, error) { return initHEIF(module, c, limit) },
			}
		}
	}
	return e
}

// Formats lists what the Engine makes previews of, for the interface.
func (e *Engine) Formats() []string {
	var f []string
	if e.pdf != nil {
		f = append(f, "pdf")
	}
	if e.heic != nil {
		f = append(f, "heic")
	}
	return f
}

// Close closes the runtimes.
func (e *Engine) Close() {
	for _, s := range []*slot{e.pdf, e.heic} {
		if s != nil {
			s.mu.Lock()
			s.close()
			s.mu.Unlock()
		}
	}
}

// PDF returns a JPEG of the first page of the PDF read from r (size bytes),
// fitting a box×box square. r is read through the job's context only; it
// may be a file on a share.
func (e *Engine) PDF(ctx context.Context, r io.ReadSeeker, size int64, box int) ([]byte, error) {
	if e.pdf == nil {
		return nil, ErrUnavailable
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// The runtime first, then the job's share: a compile charges the budget
	// while it holds the slot's lock, and a job that held its share while
	// waiting for that lock could leave the compile waiting for ever.
	res, done, err := e.pdf.acquire(ctx, e)
	if err != nil {
		return nil, err
	}
	defer done()
	pool := res.(pdfium.Pool)
	release, err := e.o.Budget(ctx, JobCost(e.o.PDFMemory))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer release()
	return renderPDF(ctx, pool, r, size, box)
}

// acquire returns the pool, preparing it first, and counts the job so that
// the runtime is not closed under it.
func (s *slot) acquire(ctx context.Context, e *Engine) (closer, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.off != nil {
		return nil, nil, s.off
	}
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.res == nil {
		if err := s.open(ctx, e); err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				s.off = fmt.Errorf("%w: %w", ErrUnavailable, err)
				e.o.Log.Warn(s.name+" previews are off", "error", err.Error())
				return nil, nil, s.off
			}
			return nil, nil, err
		}
	}
	s.jobs++
	return s.res, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.jobs--; s.jobs == 0 && s.res != nil {
			s.timer = time.AfterFunc(e.o.Idle, func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				if s.jobs == 0 {
					s.close()
				}
			})
		}
	}, nil
}

func (s *slot) close() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.res != nil {
		_ = s.res.Close()
		s.res = nil
	}
	if s.cache != nil {
		_ = s.cache.Close(context.Background())
		s.cache = nil
	}
	// The module's code and the instances' memory go back now rather than
	// at the next collection, so that an idle ferry is small again.
	runtime.GC()
	debug.FreeOSMemory()
}

// open makes the runtime: from the compiled code when it matches this
// build, otherwise by compiling — charged to the budget. Code that matches
// by its marker but that wazero refuses (damaged) is compiled anew, the
// same way.
func (s *slot) open(ctx context.Context, e *Engine) error {
	start := time.Now()
	if e.o.CodeDir != "" && markerMatches(e.o.CodeDir, s.key, s.want) {
		cache, err := wazero.NewCompilationCacheWithDir(e.o.CodeDir)
		if err == nil {
			res, err := s.load(cache)
			if err == nil {
				s.res, s.cache = res, cache
				e.o.Log.Info(s.name+" previews ready", "compiled", false, "seconds", time.Since(start).Seconds())
				return nil
			}
			_ = cache.Close(context.Background())
			e.o.Log.Warn("compiled "+s.name+" code refused, compiling anew", "error", err.Error())
		}
	}
	release, err := e.o.Budget(ctx, s.compile)
	if err != nil {
		return fmt.Errorf("no compiled code, and no budget to compile: %w", err)
	}
	defer release()
	res, err := s.load(nil)
	if err != nil {
		return err
	}
	s.res = res
	e.o.Log.Info(s.name+" previews ready", "compiled", true, "seconds", time.Since(start).Seconds())
	return nil
}

// initPDF compiles or loads pdfium. Every instance gets the memory limit,
// is aborted when its context ends, and sees no file system: go-pdfium
// would mount the host's root by default.
func initPDF(cache wazero.CompilationCache, limit int64) (pool pdfium.Pool, err error) {
	defer func() {
		// wazero panics on some damaged code rather than failing.
		if r := recover(); r != nil {
			pool, err = nil, fmt.Errorf("pdfium: %v", r)
		}
	}()
	rc := wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
		WithMemoryLimitPages(uint32(limit / 65536)).
		WithCloseOnContextDone(true)
	if cache != nil {
		rc = rc.WithCompilationCache(cache)
	}
	return webassembly.Init(webassembly.Config{
		MinIdle: 0, MaxIdle: 0, MaxTotal: 64,
		RuntimeConfig: rc,
		FSConfig:      wazero.NewFSConfig(),
		Stdout:        io.Discard,
		Stderr:        io.Discard,
	})
}

func renderPDF(ctx context.Context, pool pdfium.Pool, r io.ReadSeeker, size int64, box int) ([]byte, error) {
	inst, err := pool.GetInstanceWithContext(ctx)
	if err != nil {
		return nil, err
	}
	// A new instance for every job, killed afterwards, never handed back.
	// go-pdfium calls the module under its own context, not the job's: the
	// job's end kills the instance, and WithCloseOnContextDone turns that
	// into an abort of the call in progress.
	// Kill interrupts a call in flight but must not run twice.
	kill := sync.OnceFunc(func() { _ = inst.Kill() })
	defer kill()
	stop := context.AfterFunc(ctx, kill)
	defer stop()

	doc, err := inst.OpenDocument(&requests.OpenDocument{FileReader: r, FileReaderSize: size})
	if err != nil {
		return nil, jobError(ctx, err)
	}
	res, err := inst.RenderPageInPixels(&requests.RenderPageInPixels{
		Page:  requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: 0}},
		Width: box, Height: box,
	})
	if err != nil {
		return nil, jobError(ctx, err)
	}
	defer res.Cleanup()
	img := res.Result.RenderedImage
	if img == nil || blank(img) {
		return nil, ErrBlank
	}
	// The bitmap lives in the instance's memory until Cleanup: encode first.
	return thumb.FromImage(img, 1, box)
}

// jobError tells the job's end from a file the library could not read.
func jobError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%w: %w", ErrUnsupported, err)
}

// blank reports whether every pixel is the same colour.
func blank(img image.Image) bool {
	if rgba, ok := img.(*image.RGBA); ok {
		p := rgba.Pix
		for i := 4; i+4 <= len(p); i += 4 {
			if p[i] != p[0] || p[i+1] != p[1] || p[i+2] != p[2] || p[i+3] != p[3] {
				return false
			}
		}
		return true
	}
	b := img.Bounds()
	first := color.RGBAModel.Convert(img.At(b.Min.X, b.Min.Y))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.RGBAModel.Convert(img.At(x, y)) != first {
				return false
			}
		}
	}
	return true
}

// ── Compiled code ─────────────────────────────────────────────────────────

const markerName = "ferry-compiled.json"

// marker says what compiled code was made from: wazero's version and each
// module's, and the architecture. wazero keeps its code under its version
// and architecture already; the marker is what tells ferry, before it asks
// wazero, whether the code is there at all — a cold compile must be
// charged before it starts.
type marker struct {
	Wazero  string            `json:"wazero"`
	GOARCH  string            `json:"goarch"`
	Modules map[string]string `json:"modules"`
}

func current() marker {
	m := marker{GOARCH: runtime.GOARCH, Modules: map[string]string{}}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			v := d.Version
			if d.Replace != nil {
				v = d.Replace.Version
			}
			switch d.Path {
			case "github.com/tetratelabs/wazero":
				m.Wazero = v
			case "github.com/klippa-app/go-pdfium":
				m.Modules["pdf"] = v
			}
		}
	}
	return m
}

func markerMatches(dir, key, want string) bool {
	b, err := os.ReadFile(filepath.Join(dir, markerName))
	if err != nil {
		return false
	}
	var m marker
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	c := current()
	return c.Wazero != "" && m.Wazero == c.Wazero && m.GOARCH == c.GOARCH &&
		want != "" && m.Modules[key] == want
}

// Precompile compiles every module into dir and writes the marker last, so
// that a dir without a marker is never trusted. `ferry precompile` runs it
// when the image is built.
func Precompile(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cache, err := wazero.NewCompilationCacheWithDir(dir)
	if err != nil {
		return err
	}
	defer func() { _ = cache.Close(context.Background()) }()
	pool, err := initPDF(cache, PDFMemory)
	if err != nil {
		return err
	}
	_ = pool.Close()
	module, err := heif.Module()
	if err != nil {
		return err
	}
	h, err := initHEIF(module, cache, HEIFMemory)
	if err != nil {
		return err
	}
	_ = h.Close()
	m := current()
	m.Modules["heif"] = moduleHash(module)
	if m.Wazero == "" {
		return errors.New("preview: no build information, cannot mark the compiled code")
	}
	// wazero writes its code for its own user only (0700, 0600); the image
	// writes it as root and runs ferry as another, which must read it.
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if d.IsDir() {
			mode = 0o755
		}
		return os.Chmod(p, mode)
	})
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(filepath.Join(dir, markerName), b, 0o644)
}
