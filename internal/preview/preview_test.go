package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
	"golang.org/x/sync/semaphore"
)

// codeDir holds the compiled code TestMain makes once, as the image does.
var codeDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ferry-preview-code")
	if err != nil {
		panic(err)
	}
	if err := Precompile(dir); err != nil {
		panic(err)
	}
	codeDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// pdf builds a one-page PDF from a content stream and, given one, a JPEG
// shown as the image /Im1.
func pdf(content string, jpg []byte, w, h int) []byte {
	var objs []string
	res := "/Font << /F1 5 0 R >>"
	if jpg != nil {
		res += " /XObject << /Im1 6 0 R >>"
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << "+res+" >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content)+1, content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	if jpg != nil {
		objs = append(objs, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", w, h, len(jpg), jpg))
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	var offs []int
	for i, o := range objs {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

func invoice() []byte {
	var c strings.Builder
	c.WriteString("BT /F1 24 Tf 72 720 Td (Rechnung Fahrrad) Tj ET\n")
	for i := range 30 {
		fmt.Fprintf(&c, "BT /F1 11 Tf 72 %d Td (Position %d  Kette, Bremse  42,00 EUR) Tj ET\n", 690-i*16, i)
	}
	c.WriteString("0 0 1 RG 4 w 72 150 m 540 150 l S")
	return pdf(c.String(), nil, 0, 0)
}

// scan is a page that is one grey JPEG of w×h pixels with a dark band.
func scan(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	for y := h / 3; y < h/2; y++ {
		for x := range w {
			img.Pix[y*w+x] = 40
		}
	}
	var j bytes.Buffer
	if err := jpeg.Encode(&j, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	return pdf("q 595 0 0 842 0 0 cm /Im1 Do Q", j.Bytes(), w, h)
}

// photo is a page that is one colour JPEG of w×h pixels, as a phone's scan.
func photo(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(x*7), uint8(y*5), uint8((x+y)*3), 255
		}
	}
	var j bytes.Buffer
	if err := jpeg.Encode(&j, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	file := pdf("q 595 0 0 842 0 0 cm /Im1 Do Q", j.Bytes(), w, h)
	return bytes.Replace(file, []byte("/DeviceGray"), []byte("/DeviceRGB"), 1)
}

func engine(t *testing.T, o Options) *Engine {
	t.Helper()
	o.PDF = true
	if o.CodeDir == "" {
		o.CodeDir = codeDir
	}
	e := New(o)
	t.Cleanup(e.Close)
	return e
}

func render(t *testing.T, e *Engine, file []byte, box int) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return e.PDF(ctx, bytes.NewReader(file), int64(len(file)), box)
}

func TestPDF(t *testing.T) {
	var charged atomic.Int64
	e := engine(t, Options{Budget: func(_ context.Context, n int64) (func(), error) {
		charged.Add(n)
		return func() { charged.Add(-n) }, nil
	}})
	for _, c := range []struct {
		name string
		file []byte
		w, h int
	}{
		{"text", invoice(), 226, 320},
		{"a scan", scan(t, 2400, 1800), 226, 320},
	} {
		out, err := render(t, e, c.file, 320)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		img, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		// A4 in a 320 square: as high as the square, 0.707 as wide.
		if b := img.Bounds(); b.Dy() != c.h || b.Dx() < c.w-2 || b.Dx() > c.w+2 {
			t.Fatalf("%s: %v", c.name, b)
		}
	}
	if n := charged.Load(); n != 0 {
		t.Fatalf("%d bytes still charged", n)
	}
	if got := e.Formats(); len(got) != 1 || got[0] != "pdf" {
		t.Fatalf("formats %v", got)
	}
}

// What pdfium could not read is an error; what it rendered white — a blank
// page, or one it ran out of memory on — is ErrBlank, never a thumbnail.
func TestPDFFailures(t *testing.T) {
	e := engine(t, Options{})
	for name, c := range map[string]struct {
		file []byte
		want error
	}{
		"not a pdf": {[]byte("just text, not a PDF at all"), ErrUnsupported},
		"cut off":   {invoice()[:300], nil}, // pdfium may repair or refuse: no panic either way
		"blank":     {pdf("", nil, 0, 0), ErrBlank},
	} {
		_, err := render(t, e, c.file, 320)
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	// A page pdfium runs out of memory on comes out white, not as an
	// error: with a tight limit, a scan does (the spike found it at
	// 32 MiB for 12 megapixels).
	tight := engine(t, Options{PDFMemory: 24 << 20})
	big := photo(t, 4000, 3000)
	if _, err := render(t, tight, big, 320); !errors.Is(err, ErrBlank) {
		t.Errorf("out of memory: %v, want ErrBlank", err)
	}
	if _, err := render(t, e, big, 320); err != nil {
		t.Errorf("the same scan with the usual limit: %v", err)
	}
	// The engine is fine afterwards: every job had its own instance.
	if _, err := render(t, e, invoice(), 160); err != nil {
		t.Fatalf("after the failures: %v", err)
	}
}

// A job past its context is aborted inside the module, and the next one
// runs on a new instance.
func TestPDFTimeout(t *testing.T) {
	e := engine(t, Options{})
	big := scan(t, 5000, 4000)
	if _, err := render(t, e, invoice(), 160); err != nil { // the runtime is ready
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := e.PDF(ctx, bytes.NewReader(big), int64(len(big)), 640)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("past its context: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("aborted after %v", d)
	}
	if _, err := render(t, e, invoice(), 160); err != nil {
		t.Fatalf("after the abort: %v", err)
	}
}

// The module sees no file system: a PDF naming a host file cannot read it.
// go-pdfium mounts the host's root unless told otherwise.
func TestNoFileSystem(t *testing.T) {
	e := engine(t, Options{})
	res, done, err := e.pdf.acquire(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	inst, err := res.(pdfium.Pool).GetInstanceWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inst.Kill() }()
	host := filepath.Join(t.TempDir(), "host.pdf")
	if err := os.WriteFile(host, invoice(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openPath(inst, host); err == nil {
		t.Fatal("the module opened a file of the host")
	}
}

// Compiled code that does not match this build, or that is damaged, is not
// used: the module is compiled anew and charged — or, with no budget for
// that, the format is off and says so. Never a failed start or a panic.
func TestCompiledCode(t *testing.T) {
	stale := func(dir string) {
		b, _ := os.ReadFile(filepath.Join(dir, markerName))
		_ = os.WriteFile(filepath.Join(dir, markerName), bytes.Replace(b, []byte(`"wazero": "v`), []byte(`"wazero": "v0.0.0-old-`), 1), 0o644)
	}
	for _, c := range []struct {
		name    string
		prepare func(string)
		cold    bool
	}{
		{"matching", func(string) {}, false},
		{"damaged", damageCode, true},
		{"from another build", stale, true},
		{"none", func(dir string) { _ = os.Remove(filepath.Join(dir, markerName)) }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := copyCode(t)
			c.prepare(dir)
			var compiles atomic.Int32
			budget := func(_ context.Context, n int64) (func(), error) {
				if n == compileCost {
					compiles.Add(1)
				}
				return func() {}, nil
			}
			e := engine(t, Options{CodeDir: dir, Budget: budget})
			if _, err := render(t, e, invoice(), 160); err != nil {
				t.Fatal(err)
			}
			if cold := compiles.Load() == 1; cold != c.cold {
				t.Fatalf("compiled anew: %v, want %v", cold, c.cold)
			}
			// Without budget for a compile, the format is off: an error,
			// the same one again, no panic.
			if c.cold {
				e := engine(t, Options{CodeDir: dir, Budget: func(_ context.Context, n int64) (func(), error) {
					if n == compileCost {
						return nil, errors.New("more than the whole budget")
					}
					return func() {}, nil
				}})
				for range 2 {
					if _, err := render(t, e, invoice(), 160); !errors.Is(err, ErrUnavailable) {
						t.Fatalf("without budget: %v", err)
					}
				}
			}
		})
	}
}

// copyCode is a writable copy of the compiled code TestMain made.
func copyCode(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	_ = filepath.Walk(codeDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(codeDir, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, _ := os.ReadFile(p)
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	return dst
}

// damageCode flips bytes in the compiled code, not in its marker.
func damageCode(dir string) {
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() == markerName {
			return err
		}
		b, _ := os.ReadFile(p)
		for i := 100; i < len(b); i += 4096 {
			b[i] ^= 0xFF
		}
		return os.WriteFile(p, b, 0o644)
	})
}

// The runtime closes after a while without a job and is made again for the
// next one.
func TestIdle(t *testing.T) {
	e := engine(t, Options{Idle: 50 * time.Millisecond})
	if _, err := render(t, e, invoice(), 160); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	e.pdf.mu.Lock()
	closed := e.pdf.res == nil
	e.pdf.mu.Unlock()
	if !closed {
		t.Fatal("the runtime is still open")
	}
	if _, err := render(t, e, invoice(), 160); err != nil {
		t.Fatalf("after closing: %v", err)
	}
}

// The image writes the code as root and runs ferry as another user, which
// must be able to read all of it.
func TestPrecompiledReadable(t *testing.T) {
	n := 0
	_ = filepath.Walk(codeDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		n++
		want := os.FileMode(0o004)
		if info.IsDir() {
			want = 0o005
		}
		if info.Mode().Perm()&want != want {
			t.Errorf("%s is %v, not readable by others", p, info.Mode().Perm())
		}
		return nil
	})
	if n < 3 {
		t.Fatalf("%d entries in the compiled code", n)
	}
}

func TestBlank(t *testing.T) {
	white := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range white.Pix {
		white.Pix[i] = 255
	}
	if !blank(white) {
		t.Fatal("white is not blank")
	}
	white.Set(7, 7, color.Black)
	if blank(white) {
		t.Fatal("one black pixel is blank")
	}
	if !blank(image.NewGray(image.Rect(0, 0, 3, 3))) {
		t.Fatal("an even grey is not blank")
	}
}

func TestOff(t *testing.T) {
	e := New(Options{})
	if len(e.Formats()) != 0 {
		t.Fatal("formats while off")
	}
	if _, err := e.PDF(context.Background(), bytes.NewReader(nil), 0, 160); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("off: %v", err)
	}
}

func openPath(inst pdfium.Pdfium, path string) (*responses.OpenDocument, error) {
	return inst.OpenDocument(&requests.OpenDocument{FilePath: &path})
}

// A compile charges the budget while it holds the slot's lock: jobs must not
// hold their share while they wait for that lock, or two jobs on a budget
// that fits the compile and one job but not two jobs and the compile would
// wait for each other until their time is up.
func TestBookingOrder(t *testing.T) {
	sem := semaphore.NewWeighted(700 << 20) // a compile and one job, not two jobs and a compile
	// Each job's share is given only once both jobs hold theirs, so that the
	// test does not depend on which goroutine runs first: if jobs took their
	// share before the runtime, the compile would then wait for ever.
	var shares sync.WaitGroup
	shares.Add(2)
	budget := func(ctx context.Context, n int64) (func(), error) {
		if err := sem.Acquire(ctx, n); err != nil {
			return nil, err
		}
		if n == JobCost(PDFMemory) {
			shares.Done()
			waited := make(chan struct{})
			go func() { shares.Wait(); close(waited) }()
			select {
			case <-waited:
			case <-ctx.Done():
				sem.Release(n)
				return nil, ctx.Err()
			}
		}
		return func() { sem.Release(n) }, nil
	}
	dir := t.TempDir() // no compiled code: the first job compiles
	e := engine(t, Options{CodeDir: dir, Budget: budget, BudgetSize: 700 << 20, Concurrency: 2})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := render(t, e, invoice(), 160)
			errs <- err
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("a job: %v", err)
		}
	}
}

// A budget smaller than one page turns PDF previews off from the start: no
// error on every PDF, and the interface is not told to ask.
func TestBudgetTooSmall(t *testing.T) {
	e := New(Options{PDF: true, CodeDir: codeDir, BudgetSize: 200 << 20})
	if len(e.Formats()) != 0 {
		t.Fatalf("formats %v with a budget below a page", e.Formats())
	}
	if _, err := e.PDF(context.Background(), bytes.NewReader(invoice()), int64(len(invoice())), 160); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("%v", err)
	}
}

// In the image the compiled code is read-only. Matching code is loaded from
// it; damaged code — which wazero would delete and write anew, and cannot —
// falls back to a compile, never to an error.
func TestCompiledCodeReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only directories")
	}
	readOnly := func(t *testing.T, dir string) {
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			return os.Chmod(p, 0o444)
		})
		var dirs []string
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err == nil && info.IsDir() {
				dirs = append(dirs, p)
			}
			return err
		})
		for i := len(dirs) - 1; i >= 0; i-- {
			_ = os.Chmod(dirs[i], 0o555)
		}
		t.Cleanup(func() {
			for _, d := range dirs {
				_ = os.Chmod(d, 0o755)
			}
		})
	}
	for _, c := range []struct {
		name   string
		damage bool
	}{{"matching", false}, {"damaged", true}} {
		t.Run(c.name, func(t *testing.T) {
			dir := copyCode(t)
			if c.damage {
				damageCode(dir)
			}
			readOnly(t, dir)
			var compiles atomic.Int32
			e := engine(t, Options{CodeDir: dir, Budget: func(_ context.Context, n int64) (func(), error) {
				if n == compileCost {
					compiles.Add(1)
				}
				return func() {}, nil
			}})
			if _, err := render(t, e, invoice(), 160); err != nil {
				t.Fatal(err)
			}
			if got := compiles.Load() == 1; got != c.damage {
				t.Fatalf("compiled anew: %v, want %v", got, c.damage)
			}
		})
	}
}

// observed records what Options.Observe was told.
type observed struct {
	mu   sync.Mutex
	jobs []string // format/outcome
	runs []time.Duration
}

func (o *observed) observe(format, outcome string, _, run time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.jobs = append(o.jobs, format+"/"+outcome)
	o.runs = append(o.runs, run)
}

func (o *observed) last() (string, time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.jobs[len(o.jobs)-1], o.runs[len(o.runs)-1]
}

// A job past Options.Timeout is ErrTimeout, not a file that cannot be
// read, and the next job runs; a caller that leaves is its context's
// error, not a timeout.
func TestJobTimeout(t *testing.T) {
	var obs observed
	e := engine(t, Options{Observe: obs.observe})
	if _, err := render(t, e, invoice(), 160); err != nil { // the runtime is ready
		t.Fatal(err)
	}
	// Loading the runtime takes longer than this limit.
	e.o.Timeout = 50 * time.Millisecond
	if got, _ := obs.last(); got != "pdf/made" {
		t.Errorf("observed %q", got)
	}
	big := scan(t, 5000, 4000)
	start := time.Now()
	_, err := render(t, e, big, 640)
	if !errors.Is(err, ErrTimeout) || errors.Is(err, ErrUnsupported) {
		t.Fatalf("past the job's limit: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("aborted after %v", d)
	}
	if got, run := obs.last(); got != "pdf/timeout" || run < 50*time.Millisecond {
		t.Errorf("observed %q after %v", got, run)
	}
	if _, err := render(t, e, invoice(), 160); err != nil {
		t.Fatalf("after the timeout: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = e.PDF(ctx, bytes.NewReader(big), int64(len(big)), 640)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTimeout) {
		t.Fatalf("the caller's deadline: %v", err)
	}
	if got, _ := obs.last(); got != "pdf/cancelled" {
		t.Errorf("observed %q", got)
	}
}

func TestOutcome(t *testing.T) {
	for err, want := range map[error]string{
		nil:              "made",
		ErrTimeout:       "timeout",
		context.Canceled: "cancelled",
		ErrUnavailable:   "unavailable",
		ErrBlank:         "blank",
		ErrTooLarge:      "too_large",
		jobError(context.Background(), errors.New("x")): "unsupported",
		errors.New("the share failed"):                  "failed",
	} {
		if got := Outcome(err); got != want {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
}
