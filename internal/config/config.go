// Package config reads what `ferry serve` is configured with, from FERRY_*
// variables. docs/configuration.md describes every one.
//
// ferry has no credentials of its own: the only passwords it ever sees are
// the ones people type, and those never become configuration. The one
// secret among the variables is the cache's key (FERRY_CACHE_KEY), and only
// when the cache is on.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Config is everything `ferry serve` is configured with.
type Config struct {
	// SMBServer is host:port of the SMB server (FERRY_SMB_SERVER; the port
	// defaults to 445).
	SMBServer string
	// ServerName is what the login page calls the server
	// (FERRY_SERVER_NAME; defaults to SMBServer's host).
	ServerName string
	// Shares narrows the shares ferry lists and opens to these names,
	// compared without case (FERRY_SHARES, comma-separated; empty: all the
	// server lists).
	Shares []string

	ListenAddr    string // FERRY_LISTEN
	OpsListenAddr string // FERRY_OPS_LISTEN: health and metrics

	IdleTimeout     time.Duration // FERRY_IDLE_TIMEOUT
	SessionLifetime time.Duration // FERRY_SESSION_LIFETIME
	MaxSessions     int           // FERRY_MAX_SESSIONS
	LoginTimeout    time.Duration // FERRY_LOGIN_TIMEOUT: connect and session setup
	// RequestTimeout bounds every API request but uploads and downloads,
	// which are bounded by StallTimeout instead (FERRY_REQUEST_TIMEOUT).
	RequestTimeout time.Duration
	// StallTimeout ends an upload or download that moves no bytes for this
	// long, in either direction (FERRY_STALL_TIMEOUT).
	StallTimeout time.Duration

	MaxUpload int64 // FERRY_MAX_UPLOAD, bytes; 0: no limit
	// Thumbnails bound what one costs and what all cost together: the bytes
	// read (FERRY_THUMBNAIL_MAX_BYTES), the pixels decoded
	// (FERRY_THUMBNAIL_MAX_PIXELS), the memory all thumbnails in the making
	// may take, by their estimates (FERRY_THUMBNAIL_MEMORY), and the decodes
	// in parallel (FERRY_THUMBNAIL_CONCURRENCY). docs/operations.md has the
	// measurements.
	ThumbnailMaxBytes    int64
	ThumbnailMaxPixels   int
	ThumbnailMemory      int64
	ThumbnailConcurrency int
	MaxListing           int // FERRY_MAX_LISTING: entries in one folder listing
	MaxDelete            int // FERRY_MAX_DELETE: entries in one recursive delete
	MaxArchive           int // FERRY_MAX_ARCHIVE: entries in one ZIP download
	// The cache of thumbnails and details: a directory (FERRY_CACHE_DIR;
	// empty: none), its size (FERRY_CACHE_MAX_BYTES) and the key that names
	// and encrypts its entries (FERRY_CACHE_KEY, at least 32 bytes).
	CacheDir      string
	CacheMaxBytes int64
	CacheKey      string
	// Previews of formats Go cannot decode, run as WebAssembly: PDF's first
	// page (FERRY_PREVIEW_PDF), how many at once (FERRY_PREVIEW_CONCURRENCY),
	// how long one may run (FERRY_PREVIEW_TIMEOUT), and where the compiled
	// code is (FERRY_WASM_DIR; the image ships it).
	PreviewPDF         bool
	PreviewConcurrency int
	PreviewTimeout     time.Duration
	WASMDir            string
	// HEIC photos (FERRY_PREVIEW_HEIC), the memory one may take
	// (FERRY_PREVIEW_HEIC_MEMORY), and a module to use instead of the one
	// ferry carries (FERRY_HEIF_WASM, a file; third_party/heif/README.md).
	PreviewHEIC       bool
	PreviewHEICMemory int64
	HEIFWASM          string
	// SMBConnections is how many SMB connections a login sets up
	// (FERRY_SMB_CONNECTIONS, 1 to 4): one for browsing, the others for
	// thumbnails, details and content (smb.Group).
	SMBConnections int

	// LoginRatePerAddress and LoginRatePerUser bound login attempts per
	// minute, per client address and per username (FERRY_LOGIN_RATE_ADDRESS,
	// FERRY_LOGIN_RATE_USER).
	LoginRatePerAddress int
	LoginRatePerUser    int

	// TrustedProxies are the addresses whose X-Forwarded-For ferry believes
	// for the client's address (FERRY_TRUSTED_PROXIES, comma-separated
	// CIDRs). Without them the client is the TCP peer.
	TrustedProxies []netip.Prefix
	// CookieSecure is "auto" (Secure when the request came over HTTPS, as
	// the TLS state or X-Forwarded-Proto says), "true" or "false"
	// (FERRY_COOKIE_SECURE).
	CookieSecure string

	LogLevel  string // FERRY_LOG_LEVEL
	LogFormat string // FERRY_LOG_FORMAT: json or text
}

// FromEnv reads the configuration through getenv (os.Getenv).
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		ListenAddr:      or(getenv("FERRY_LISTEN"), ":8080"),
		OpsListenAddr:   or(getenv("FERRY_OPS_LISTEN"), ":8081"),
		CookieSecure:    or(getenv("FERRY_COOKIE_SECURE"), "auto"),
		LogLevel:        or(getenv("FERRY_LOG_LEVEL"), "info"),
		LogFormat:       or(getenv("FERRY_LOG_FORMAT"), "json"),
		ServerName:      getenv("FERRY_SERVER_NAME"),
		IdleTimeout:     30 * time.Minute,
		SessionLifetime: 12 * time.Hour,
		MaxSessions:     200,
		LoginTimeout:    10 * time.Second,
		RequestTimeout:  60 * time.Second,
		StallTimeout:    60 * time.Second,
		MaxUpload:       0,
		// A listing is written out as it is read, so the bound protects the
		// browser, which holds the folder, rather than ferry.
		MaxListing:           200000,
		MaxDelete:            10000,
		MaxArchive:           10000,
		SMBConnections:       3,
		ThumbnailMaxBytes:    64 << 20,
		ThumbnailMaxPixels:   50_000_000,
		ThumbnailMemory:      320 << 20,
		ThumbnailConcurrency: 2,
		CacheMaxBytes:        1 << 30,
		PreviewPDF:           true,
		PreviewHEIC:          true,
		PreviewHEICMemory:    96 << 20,
		PreviewConcurrency:   1,
		PreviewTimeout:       30 * time.Second,
		WASMDir:              "/usr/lib/ferry/wasm",
		LoginRatePerAddress:  10,
		LoginRatePerUser:     5,
	}
	var errs []error
	fail := func(name string, err error) { errs = append(errs, fmt.Errorf("%s: %w", name, err)) }

	server := strings.TrimSpace(getenv("FERRY_SMB_SERVER"))
	if server == "" {
		fail("FERRY_SMB_SERVER", errors.New("is required"))
	} else {
		hp, err := hostPort(server)
		if err != nil {
			fail("FERRY_SMB_SERVER", err)
		}
		c.SMBServer = hp
		if c.ServerName == "" {
			c.ServerName, _, _ = net.SplitHostPort(hp)
		}
	}
	for _, s := range strings.Split(getenv("FERRY_SHARES"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			c.Shares = append(c.Shares, s)
		}
	}

	durations := []struct {
		name string
		dst  *time.Duration
	}{
		{"FERRY_IDLE_TIMEOUT", &c.IdleTimeout},
		{"FERRY_SESSION_LIFETIME", &c.SessionLifetime},
		{"FERRY_LOGIN_TIMEOUT", &c.LoginTimeout},
		{"FERRY_REQUEST_TIMEOUT", &c.RequestTimeout},
		{"FERRY_STALL_TIMEOUT", &c.StallTimeout},
		{"FERRY_PREVIEW_TIMEOUT", &c.PreviewTimeout},
	}
	for _, d := range durations {
		if v := getenv(d.name); v != "" {
			p, err := time.ParseDuration(v)
			if err != nil || p <= 0 {
				fail(d.name, fmt.Errorf("%q is not a positive duration", v))
				continue
			}
			*d.dst = p
		}
	}
	ints := []struct {
		name string
		dst  *int
		min  int
	}{
		{"FERRY_MAX_SESSIONS", &c.MaxSessions, 1},
		{"FERRY_MAX_LISTING", &c.MaxListing, 1},
		{"FERRY_MAX_DELETE", &c.MaxDelete, 1},
		{"FERRY_MAX_ARCHIVE", &c.MaxArchive, 1},
		{"FERRY_LOGIN_RATE_ADDRESS", &c.LoginRatePerAddress, 1},
		{"FERRY_LOGIN_RATE_USER", &c.LoginRatePerUser, 1},
		{"FERRY_THUMBNAIL_MAX_PIXELS", &c.ThumbnailMaxPixels, 1},
		{"FERRY_THUMBNAIL_CONCURRENCY", &c.ThumbnailConcurrency, 1},
		{"FERRY_PREVIEW_CONCURRENCY", &c.PreviewConcurrency, 1},
	}
	for _, i := range ints {
		if v := getenv(i.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < i.min {
				fail(i.name, fmt.Errorf("%q is not an integer of at least %d", v, i.min))
				continue
			}
			*i.dst = n
		}
	}
	if v := getenv("FERRY_MAX_UPLOAD"); v != "" {
		n, err := parseBytes(v)
		if err != nil {
			fail("FERRY_MAX_UPLOAD", err)
		}
		c.MaxUpload = n
	}
	if v := getenv("FERRY_SMB_CONNECTIONS"); v != "" {
		// At most smb.MaxConnections: each is one more connection, and one
		// more session the server keeps, for every person logged in.
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 4 {
			fail("FERRY_SMB_CONNECTIONS", fmt.Errorf("%q is not an integer from 1 to 4", v))
		} else {
			c.SMBConnections = n
		}
	}
	if v := getenv("FERRY_THUMBNAIL_MAX_BYTES"); v != "" {
		n, err := parseBytes(v)
		if err != nil || n == 0 {
			fail("FERRY_THUMBNAIL_MAX_BYTES", fmt.Errorf("%q is not a positive byte count like 64Mi", v))
		}
		c.ThumbnailMaxBytes = n
	}
	if v := getenv("FERRY_THUMBNAIL_MEMORY"); v != "" {
		n, err := parseBytes(v)
		if err != nil || n == 0 {
			fail("FERRY_THUMBNAIL_MEMORY", fmt.Errorf("%q is not a positive byte count like 320Mi", v))
		}
		c.ThumbnailMemory = n
	}
	if v := getenv("FERRY_PREVIEW_PDF"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			fail("FERRY_PREVIEW_PDF", fmt.Errorf("%q is not true or false", v))
		}
		c.PreviewPDF = b
	}
	if v := getenv("FERRY_PREVIEW_HEIC"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			fail("FERRY_PREVIEW_HEIC", fmt.Errorf("%q is not true or false", v))
		}
		c.PreviewHEIC = b
	}
	if v := getenv("FERRY_PREVIEW_HEIC_MEMORY"); v != "" {
		n, err := parseBytes(v)
		if err != nil || n < 32<<20 {
			fail("FERRY_PREVIEW_HEIC_MEMORY", fmt.Errorf("%q is not a byte count of at least 32Mi", v))
		}
		c.PreviewHEICMemory = n
	}
	c.HEIFWASM = strings.TrimSpace(getenv("FERRY_HEIF_WASM"))
	if v := strings.TrimSpace(getenv("FERRY_WASM_DIR")); v != "" {
		c.WASMDir = v
	}
	c.CacheDir = strings.TrimSpace(getenv("FERRY_CACHE_DIR"))
	c.CacheKey = getenv("FERRY_CACHE_KEY")
	if v := getenv("FERRY_CACHE_MAX_BYTES"); v != "" {
		n, err := parseBytes(v)
		if err != nil || n == 0 {
			fail("FERRY_CACHE_MAX_BYTES", fmt.Errorf("%q is not a positive byte count like 1Gi", v))
		}
		c.CacheMaxBytes = n
	}
	if c.CacheDir != "" && len(c.CacheKey) < 32 {
		// Without a key the entries could be neither named nor encrypted;
		// a made-up one would lose them all at the next start.
		fail("FERRY_CACHE_KEY", errors.New("is required with FERRY_CACHE_DIR, at least 32 bytes"))
	}
	for _, s := range strings.Split(getenv("FERRY_TRUSTED_PROXIES"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			a, aerr := netip.ParseAddr(s)
			if aerr != nil {
				fail("FERRY_TRUSTED_PROXIES", fmt.Errorf("%q is not an address or CIDR", s))
				continue
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, p.Masked())
	}
	switch c.CookieSecure {
	case "auto", "true", "false":
	default:
		fail("FERRY_COOKIE_SECURE", fmt.Errorf("%q is not auto, true or false", c.CookieSecure))
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		fail("FERRY_LOG_FORMAT", fmt.Errorf("%q is not json or text", c.LogFormat))
	}
	if c.PreviewTimeout < time.Second {
		// Shorter, and a PDF would not get through opening its runtime.
		fail("FERRY_PREVIEW_TIMEOUT", errors.New("is shorter than 1s"))
	}
	if c.SessionLifetime < c.IdleTimeout {
		fail("FERRY_SESSION_LIFETIME", errors.New("is shorter than FERRY_IDLE_TIMEOUT"))
	}
	return c, errors.Join(errs...)
}

func or(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

// hostPort adds SMB's port to a server given without one.
func hostPort(s string) (string, error) {
	if h, p, err := net.SplitHostPort(s); err == nil {
		if h == "" {
			return "", fmt.Errorf("%q has no host", s)
		}
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("%q has no valid port", s)
		}
		return s, nil
	}
	if strings.ContainsAny(s, "/ ") {
		return "", fmt.Errorf("%q is not host or host:port", s)
	}
	return net.JoinHostPort(strings.Trim(s, "[]"), "445"), nil
}

// parseBytes reads a byte count with an optional binary suffix: 500, 512Ki,
// 100Mi, 4Gi, 1Ti.
func parseBytes(v string) (int64, error) {
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}} {
		if strings.HasSuffix(v, u.suffix) {
			mult, v = u.mult, strings.TrimSuffix(v, u.suffix)
			break
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || n > (1<<62)/mult {
		return 0, fmt.Errorf("%q is not a byte count like 4Gi", v)
	}
	return n * mult, nil
}

// AllowsShare reports whether a share may be listed and opened under
// FERRY_SHARES.
func (c Config) AllowsShare(name string) bool {
	if len(c.Shares) == 0 {
		return true
	}
	for _, s := range c.Shares {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}
