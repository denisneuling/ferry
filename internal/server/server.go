// Package server runs ferry's two listeners: the interface and the API on
// one, health (and later metrics) on the other, so that probes never compete
// with people for the first and the second can stay inside the cluster.
package server

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/denisneuling/ferry/internal/api"
	"github.com/denisneuling/ferry/internal/config"
	"github.com/denisneuling/ferry/internal/metrics"
	"github.com/denisneuling/ferry/internal/session"
	"github.com/denisneuling/ferry/internal/tracing"
	"github.com/denisneuling/ferry/internal/ui"
)

// Server is ferry's HTTP side.
type Server struct {
	cfg      config.Config
	sessions *session.Table
	log      *slog.Logger
	public   *http.Server
	ops      *http.Server
}

// New wires the API onto the public listener, and the probes and the
// metrics onto the operations listener. m may be nil.
func New(cfg config.Config, sessions *session.Table, a *api.API, m *metrics.Metrics, log *slog.Logger) *Server {
	mux := http.NewServeMux()
	mux.Handle("/api/", a.Handler())
	// Everything else is the interface, and its routes.
	mux.Handle("/", ui.Handler())

	ops := http.NewServeMux()
	ops.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	ops.Handle("GET /readyz", newReadiness(cfg.SMBServer))
	if m != nil {
		ops.Handle("GET /metrics", m.Handler())
	}

	return &Server{
		cfg:      cfg,
		sessions: sessions,
		log:      log,
		public:   newHTTPServer(cfg.ListenAddr, recoverPanics(log, observe(log, m, securityHeaders(mux)))),
		ops:      newHTTPServer(cfg.OpsListenAddr, recoverPanics(log, ops)),
	}
}

// newHTTPServer bounds what a client can make the server wait for before a
// handler runs. There is no overall read or write timeout: uploads and
// downloads take as long as they take, and are bounded per chunk instead
// (internal/api's stall timeout).
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
}

// Run serves until ctx ends, then shuts down: open requests get a moment to
// finish, and every session ends with the process.
func (s *Server) Run(ctx context.Context) error {
	errc := make(chan error, 2)
	var wg sync.WaitGroup
	for _, srv := range []*http.Server{s.public, s.ops} {
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			_ = s.public.Close()
			_ = s.ops.Close()
			return err
		}
		s.log.Info("listening", "addr", ln.Addr().String())
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}
	sweepCtx, stopSweep := context.WithCancel(context.Background())
	sweepDone := make(chan struct{})
	go func() {
		s.sessions.Run(sweepCtx, 30*time.Second)
		close(sweepDone)
	}()

	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.public.Shutdown(shutdown)
	_ = s.ops.Shutdown(shutdown)
	wg.Wait()
	stopSweep()
	<-sweepDone // Run ends every session as it returns.
	return err
}

// securityHeaders apply to everything the public listener answers; the API
// and file downloads tighten them further.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// statusRecorder remembers the status for the request log. It passes
// Unwrap through, so http.ResponseController reaches the connection.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// observe traces, counts and logs every request by its route pattern —
// never by its path or query, which carry share, folder and file names: a
// span named after the route, the request metrics, and a log line with the
// status, the bytes and the time.
func observe(log *slog.Logger, m *metrics.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx, span := tracing.Tracer().Start(r.Context(), "HTTP "+r.Method, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		r = r.WithContext(ctx)
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		span.SetName(route)
		span.SetAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", rec.status),
		)
		if rec.status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(rec.status))
		}
		m.Request(r.Method, route, rec.status, time.Since(start))
		log.Debug("request", "method", r.Method, "route", route, "status", rec.status,
			"bytes", rec.bytes, "duration_ms", time.Since(start).Milliseconds())
	})
}

// recoverPanics turns a panic into a 500 and a log line with the stack, and
// keeps the process up. The request's details stay out of the line.
func recoverPanics(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic", "route", r.Pattern, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// readiness reports whether the SMB server's port answers, checked at most
// every few seconds whatever the probe rate.
type readiness struct {
	addr string

	mu      sync.Mutex
	checked time.Time
	err     error
}

func newReadiness(addr string) *readiness { return &readiness{addr: addr} }

func (r *readiness) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	if time.Since(r.checked) > 5*time.Second {
		c, err := net.DialTimeout("tcp", r.addr, 2*time.Second)
		if err == nil {
			_ = c.Close()
		}
		r.err, r.checked = err, time.Now()
	}
	err := r.err
	r.mu.Unlock()
	if err != nil {
		http.Error(w, "the SMB server does not answer\n", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok\n"))
}
