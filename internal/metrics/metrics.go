// Package metrics exposes ferry's Prometheus metrics (CONCEPT §6) on the
// operations listener: sessions, logins by outcome, bytes up and down, SMB
// errors by kind, thumbnails and their memory, and the HTTP API by route.
// Everything is prefixed ferry_.
//
// No label ever carries a user name, a share, a folder, a file name or a
// path: routes are patterns (/api/v1/shares/{share}/content), errors are
// kinds from a fixed list.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the registry and the instruments ferry updates. A nil
// *Metrics records nothing, which tests rely on.
type Metrics struct {
	registry *prometheus.Registry

	logins      *prometheus.CounterVec
	bytes       *prometheus.CounterVec
	smbErrors   *prometheus.CounterVec
	thumbnails  *prometheus.CounterVec
	httpCount   *prometheus.CounterVec
	httpLatency *prometheus.HistogramVec
	smbConns    prometheus.Gauge
	smbLost     *prometheus.CounterVec
	smbWait     *prometheus.HistogramVec
	archives    *prometheus.CounterVec
	cacheReqs   *prometheus.CounterVec
	cacheBytes  prometheus.Gauge
	cacheEvict  prometheus.Counter
}

// Gauges are read at every scrape from where they are kept.
type Gauges struct {
	Sessions        func() int
	ThumbnailMemory func() (used, limit int64)
}

// New registers every metric; the build is reported as ferry_build_info.
func New(version, commit string, g Gauges) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := &Metrics{
		registry: reg,
		logins: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ferry_logins_total",
			Help: "Login attempts by outcome: ok, auth (refused by the server), guest (the server offered only a guest session), unreachable, rate_limited, too_many_sessions, invalid, internal.",
		}, []string{"outcome"}),
		bytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ferry_transfer_bytes_total",
			Help: "File bytes moved between the SMB server and browsers: up (uploads) and down (downloads, previews, thumbnails' sources not included).",
		}, []string{"direction"}),
		smbErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ferry_smb_errors_total",
			Help: "Errors of SMB operations answered to a request, by kind: access_denied, not_found, exists, busy, not_empty, not_dir, is_dir, invalid_path, limit, unreachable, session_lost, canceled, internal.",
		}, []string{"kind"}),
		thumbnails: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ferry_thumbnails_total",
			Help: "Thumbnail requests by outcome: made, not_modified, unsupported, too_large, failed.",
		}, []string{"outcome"}),
		httpCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ferry_http_requests_total",
			Help: "HTTP requests, by method, route pattern and status code.",
		}, []string{"method", "route", "code"}),
		httpLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "ferry_http_request_duration_seconds",
			Help: "Duration of HTTP requests, by route pattern. Downloads and uploads count until their last byte.",
			// From a listing that answers in milliseconds to a download that
			// takes minutes.
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 300},
		}, []string{"route"}),
		smbConns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ferry_smb_connections",
			Help: "SMB connections open now, of all sessions together: up to FERRY_SMB_CONNECTIONS each.",
		}),
		smbLost: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ferry_smb_connections_lost_total",
			Help: "SMB connections that went away on their own, by pool: interactive (the session ends) or background (the session carries on with fewer).",
		}, []string{"pool"}),
		smbWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "ferry_smb_slot_wait_seconds",
			Help: "How long a request waited for its SMB connection's one request slot, by pool: interactive or background.",
			// From a free slot to one behind a slow server's call.
			Buckets: []float64{.0005, .001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"pool"}),
		archives: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ferry_archives_total",
			Help: "ZIP downloads by outcome: ok, failed (a file failed once the archive had begun; the download was broken off), canceled (the client left).",
		}, []string{"outcome"}),
		cacheReqs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ferry_cache_requests_total",
			Help: "Lookups in the cache of thumbnails and details, by kind (thumbnail, metadata) and outcome: hit, miss, error (the disk failed; a failed write counts too).",
		}, []string{"kind", "outcome"}),
		cacheBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ferry_cache_bytes",
			Help: "Bytes in the cache's entries, as last counted.",
		}),
		cacheEvict: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ferry_cache_evictions_total",
			Help: "Entries removed from the cache to keep it within FERRY_CACHE_MAX_BYTES, the least recently used first.",
		}),
	}
	reg.MustRegister(m.logins, m.bytes, m.smbErrors, m.thumbnails, m.httpCount, m.httpLatency, m.smbConns, m.smbLost, m.smbWait, m.archives,
		m.cacheReqs, m.cacheBytes, m.cacheEvict)
	build := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "ferry_build_info",
		Help:        "Always 1; the labels say which build runs.",
		ConstLabels: prometheus.Labels{"version": version, "commit": commit},
	})
	build.Set(1)
	reg.MustRegister(build)
	if g.Sessions != nil {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "ferry_sessions",
			Help: "Sessions open now, each one person's login with its SMB connections.",
		}, func() float64 { return float64(g.Sessions()) }))
	}
	if g.ThumbnailMemory != nil {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "ferry_thumbnail_memory_bytes",
			Help: "Memory taken from the thumbnail budget now, by the estimates of the thumbnails in the making.",
		}, func() float64 { used, _ := g.ThumbnailMemory(); return float64(used) }))
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "ferry_thumbnail_memory_limit_bytes",
			Help: "The thumbnail budget, FERRY_THUMBNAIL_MEMORY.",
		}, func() float64 { _, limit := g.ThumbnailMemory(); return float64(limit) }))
	}
	return m
}

// Handler serves the metrics for Prometheus to scrape.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Login counts a login attempt by its outcome.
func (m *Metrics) Login(outcome string) {
	if m != nil {
		m.logins.WithLabelValues(outcome).Inc()
	}
}

// Up and Down count file bytes.
func (m *Metrics) Up(n int64) {
	if m != nil && n > 0 {
		m.bytes.WithLabelValues("up").Add(float64(n))
	}
}

func (m *Metrics) Down(n int64) {
	if m != nil && n > 0 {
		m.bytes.WithLabelValues("down").Add(float64(n))
	}
}

// SMBError counts an SMB operation's error by its kind.
func (m *Metrics) SMBError(kind string) {
	if m != nil {
		m.smbErrors.WithLabelValues(kind).Inc()
	}
}

// Thumbnail counts a thumbnail request by its outcome.
func (m *Metrics) Thumbnail(outcome string) {
	if m != nil {
		m.thumbnails.WithLabelValues(outcome).Inc()
	}
}

// Request records an HTTP request; route is the matched pattern, or
// "unmatched".
func (m *Metrics) Request(method, route string, code int, d time.Duration) {
	if m == nil {
		return
	}
	m.httpCount.WithLabelValues(method, route, strconv.Itoa(code)).Inc()
	m.httpLatency.WithLabelValues(route).Observe(d.Seconds())
}

// Connections counts SMB connections opened (positive) and closed (negative).
func (m *Metrics) Connections(delta int) {
	if m != nil {
		m.smbConns.Add(float64(delta))
	}
}

// ConnectionLost counts an SMB connection that went away on its own.
func (m *Metrics) ConnectionLost(pool string) {
	if m != nil {
		m.smbLost.WithLabelValues(pool).Inc()
	}
}

// SlotWait records how long a request waited for its connection's slot.
func (m *Metrics) SlotWait(pool string, d time.Duration) {
	if m != nil {
		m.smbWait.WithLabelValues(pool).Observe(d.Seconds())
	}
}

// Archive counts a ZIP download by its outcome.
func (m *Metrics) Archive(outcome string) {
	if m != nil {
		m.archives.WithLabelValues(outcome).Inc()
	}
}

// CacheRequest counts a lookup in the cache, or a failed write to it.
func (m *Metrics) CacheRequest(kind, outcome string) {
	if m != nil {
		m.cacheReqs.WithLabelValues(kind, outcome).Inc()
	}
}

// CacheBytes sets what the cache's entries take.
func (m *Metrics) CacheBytes(n int64) {
	if m != nil {
		m.cacheBytes.Set(float64(n))
	}
}

// CacheEvicted counts entries removed to keep the cache within its size.
func (m *Metrics) CacheEvicted(n int) {
	if m != nil {
		m.cacheEvict.Add(float64(n))
	}
}
