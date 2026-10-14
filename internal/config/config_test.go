package config

import (
	"strings"
	"testing"
)

// These cases protect deployment input and migration aliases. A malformed boolean,
// ignored explicit override, or insecure secret must fail before opening a port.
// There was no existing configuration coverage and no production test seam is used.
func TestDeploymentConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		wantError string
	}{
		{"valid", nil, ""},
		{"short secret", map[string]string{"SECRET_KEY": "change_me"}, "SECRET_KEY"},
		{"invalid boolean", map[string]string{"GLITCHTIP_ENABLE_LOGS": "sometimes"}, "LOGS"},
		{"invalid port", map[string]string{"PORT": "70000"}, "PORT"},
		{"bad domain", map[string]string{"GOODSENTRY_DOMAIN": "ftp://example.com"}, "DOMAIN"},
		{"bad database", map[string]string{"DATABASE_URL": "http://localhost/db"}, "DATABASE_URL"},
		{"no monitor workers", map[string]string{"MONITOR_WORKERS": "0"}, "MONITOR_WORKERS"},
		{"too many monitor workers", map[string]string{"MONITOR_WORKERS": "17"}, "MONITOR_WORKERS"},
		{"invalid private network policy", map[string]string{"GOODSENTRY_UPTIME_ALLOW_PRIVATE_IPS": "perhaps"}, "ALLOW_PRIVATE_IPS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
			t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
			t.Setenv("GOODSENTRY_DOMAIN", "https://errors.example.com/monitoring/")
			t.Setenv("GLITCHTIP_ENABLE_LOGS", "False")
			t.Setenv("GOODSENTRY_ENABLE_LOGS", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			c, err := Load()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v; want %s", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Domain != "https://errors.example.com/monitoring" || c.Prefix != "/monitoring" || c.Logs {
				t.Fatalf("wrong deployment configuration: domain=%s prefix=%s logs=%v", c.Domain, c.Prefix, c.Logs)
			}
		})
	}
}

func TestExplicitGoodSentryFlagOverridesAlias(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
	t.Setenv("GOODSENTRY_DOMAIN", "http://localhost:8000")
	t.Setenv("GLITCHTIP_ENABLE_LOGS", "true")
	t.Setenv("GOODSENTRY_ENABLE_LOGS", "false")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Logs {
		t.Fatal("explicit false was ignored")
	}
}

func TestUptimeDeploymentOptions(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
	t.Setenv("GOODSENTRY_DOMAIN", "http://localhost:8000")
	t.Setenv("MONITOR_WORKERS", "16")
	t.Setenv("GLITCHTIP_UPTIME_ALLOW_PRIVATE_IPS", "true")
	t.Setenv("GOODSENTRY_UPTIME_ALLOW_PRIVATE_IPS", "")
	c, err := Load()
	if err != nil || !c.UptimeAllowPrivate || c.MonitorWorkers != 16 {
		t.Fatalf("uptime deployment options=%+v err=%v", c, err)
	}
	t.Setenv("GOODSENTRY_UPTIME_ALLOW_PRIVATE_IPS", "false")
	c, err = Load()
	if err != nil || c.UptimeAllowPrivate {
		t.Fatalf("explicit private-network policy ignored: %v", err)
	}
}
