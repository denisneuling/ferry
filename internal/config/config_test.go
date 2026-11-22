package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"FERRY_SMB_SERVER": "nas.example.lan"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.SMBServer != "nas.example.lan:445" || c.ServerName != "nas.example.lan" {
		t.Errorf("server %q, name %q", c.SMBServer, c.ServerName)
	}
	if c.IdleTimeout != 30*time.Minute || c.SessionLifetime != 12*time.Hour {
		t.Errorf("timeouts %v %v", c.IdleTimeout, c.SessionLifetime)
	}
	if c.ListenAddr != ":8080" || c.OpsListenAddr != ":8081" || c.CookieSecure != "auto" {
		t.Errorf("listen %q %q cookie %q", c.ListenAddr, c.OpsListenAddr, c.CookieSecure)
	}
	if !c.AllowsShare("anything") {
		t.Error("an empty FERRY_SHARES must allow every share")
	}
	if c.MaxListing != 200000 || c.MaxDelete != 10000 {
		t.Errorf("max listing %d, max delete %d", c.MaxListing, c.MaxDelete)
	}
	if c.SMBConnections != 3 || c.MaxArchive != 10000 {
		t.Errorf("smb connections %d", c.SMBConnections)
	}
	if !c.PreviewPDF || c.PreviewConcurrency != 1 || c.WASMDir != "/usr/lib/ferry/wasm" {
		t.Errorf("previews %v %d %q", c.PreviewPDF, c.PreviewConcurrency, c.WASMDir)
	}
	if c.CacheDir != "" || c.CacheMaxBytes != 1<<30 {
		t.Errorf("cache %q %d", c.CacheDir, c.CacheMaxBytes)
	}
}

func TestValues(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"FERRY_SMB_SERVER":      "[fd00::5]:4445",
		"FERRY_SERVER_NAME":     "The NAS",
		"FERRY_SHARES":          " alex, Family ,,",
		"FERRY_IDLE_TIMEOUT":    "10m",
		"FERRY_MAX_UPLOAD":      "4Gi",
		"FERRY_TRUSTED_PROXIES": "198.51.100.0/24, 192.0.2.7",
		"FERRY_COOKIE_SECURE":   "true",
		"FERRY_MAX_LISTING":     "500000",
		"FERRY_MAX_DELETE":      "25",
		"FERRY_SMB_CONNECTIONS": "1",
		"FERRY_CACHE_DIR":       "/var/cache/ferry",
		"FERRY_CACHE_MAX_BYTES": "10Gi",
		"FERRY_CACHE_KEY":       strings.Repeat("k", 32),
		"FERRY_PREVIEW_PDF":     "false",
		"FERRY_WASM_DIR":        "/opt/wasm",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.SMBServer != "[fd00::5]:4445" || c.ServerName != "The NAS" {
		t.Errorf("server %q name %q", c.SMBServer, c.ServerName)
	}
	if !c.AllowsShare("FAMILY") || !c.AllowsShare("alex") || c.AllowsShare("Public") {
		t.Errorf("shares %v", c.Shares)
	}
	if c.MaxUpload != 4<<30 || c.IdleTimeout != 10*time.Minute {
		t.Errorf("max upload %d idle %v", c.MaxUpload, c.IdleTimeout)
	}
	if c.MaxListing != 500000 || c.MaxDelete != 25 || c.SMBConnections != 1 {
		t.Errorf("max listing %d, max delete %d, smb connections %d", c.MaxListing, c.MaxDelete, c.SMBConnections)
	}
	if c.PreviewPDF || c.WASMDir != "/opt/wasm" {
		t.Errorf("previews %v %q", c.PreviewPDF, c.WASMDir)
	}
	if c.CacheDir != "/var/cache/ferry" || c.CacheMaxBytes != 10<<30 || len(c.CacheKey) != 32 {
		t.Errorf("cache %q %d", c.CacheDir, c.CacheMaxBytes)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].String() != "192.0.2.7/32" {
		t.Errorf("proxies %v", c.TrustedProxies)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"FERRY_SMB_SERVER":      {},
		"has no host":           {"FERRY_SMB_SERVER": ":445"},
		"from 1 to 4":           {"FERRY_SMB_SERVER": "nas", "FERRY_SMB_CONNECTIONS": "5"},
		"no valid port":         {"FERRY_SMB_SERVER": "nas:99999"},
		"positive duration":     {"FERRY_SMB_SERVER": "nas", "FERRY_IDLE_TIMEOUT": "soon"},
		"byte count":            {"FERRY_SMB_SERVER": "nas", "FERRY_MAX_UPLOAD": "4GB"},
		"address or CIDR":       {"FERRY_SMB_SERVER": "nas", "FERRY_TRUSTED_PROXIES": "proxy"},
		"auto, true or false":   {"FERRY_SMB_SERVER": "nas", "FERRY_COOKIE_SECURE": "yes"},
		"shorter than":          {"FERRY_SMB_SERVER": "nas", "FERRY_SESSION_LIFETIME": "5m"},
		"integer of at least 1": {"FERRY_SMB_SERVER": "nas", "FERRY_MAX_SESSIONS": "0"},
		"FERRY_CACHE_KEY":       {"FERRY_SMB_SERVER": "nas", "FERRY_CACHE_DIR": "/c", "FERRY_CACHE_KEY": "short"},
		"like 1Gi":              {"FERRY_SMB_SERVER": "nas", "FERRY_CACHE_MAX_BYTES": "1GB"},
		"true or false":         {"FERRY_SMB_SERVER": "nas", "FERRY_PREVIEW_PDF": "maybe"},
	}
	for want, e := range cases {
		_, err := FromEnv(env(e))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: got %v, want an error containing %q", e, err, want)
		}
	}
}
