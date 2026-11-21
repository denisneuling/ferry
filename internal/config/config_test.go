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
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].String() != "192.0.2.7/32" {
		t.Errorf("proxies %v", c.TrustedProxies)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"FERRY_SMB_SERVER":      {},
		"has no host":           {"FERRY_SMB_SERVER": ":445"},
		"no valid port":         {"FERRY_SMB_SERVER": "nas:99999"},
		"positive duration":     {"FERRY_SMB_SERVER": "nas", "FERRY_IDLE_TIMEOUT": "soon"},
		"byte count":            {"FERRY_SMB_SERVER": "nas", "FERRY_MAX_UPLOAD": "4GB"},
		"address or CIDR":       {"FERRY_SMB_SERVER": "nas", "FERRY_TRUSTED_PROXIES": "proxy"},
		"auto, true or false":   {"FERRY_SMB_SERVER": "nas", "FERRY_COOKIE_SECURE": "yes"},
		"shorter than":          {"FERRY_SMB_SERVER": "nas", "FERRY_SESSION_LIFETIME": "5m"},
		"integer of at least 1": {"FERRY_SMB_SERVER": "nas", "FERRY_MAX_SESSIONS": "0"},
	}
	for want, e := range cases {
		_, err := FromEnv(env(e))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: got %v, want an error containing %q", e, err, want)
		}
	}
}
