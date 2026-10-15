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
		{"too many alert workers", map[string]string{"ALERT_WORKERS": "17"}, "ALERT_WORKERS"},
		{"no alert attempts", map[string]string{"ALERT_MAX_ATTEMPTS": "0"}, "ALERT_MAX_ATTEMPTS"},
		{"unbounded delivery timeout", map[string]string{"ALERT_DELIVERY_TIMEOUT_SECONDS": "61"}, "ALERT_DELIVERY_TIMEOUT_SECONDS"},
		{"bad webhook policy", map[string]string{"GOODSENTRY_WEBHOOK_ALLOW_PRIVATE_IPS": "perhaps"}, "ALLOW_PRIVATE_IPS"},
		{"unsupported mail", map[string]string{"EMAIL_URL": "http://mail.example.test"}, "EMAIL_URL"},
		{"mail header injection", map[string]string{"DEFAULT_FROM_EMAIL": "sender@example.test\r\nBcc: victim@example.test"}, "DEFAULT_FROM_EMAIL"},
		{"expired upload lifetime", map[string]string{"ARTIFACT_CHUNK_TTL_HOURS": "0"}, "ARTIFACT_CHUNK_TTL_HOURS"},
		{"unbounded upload lifetime", map[string]string{"ARTIFACT_CHUNK_TTL_HOURS": "169"}, "ARTIFACT_CHUNK_TTL_HOURS"},
		{"invalid staging bytes", map[string]string{"ARTIFACT_STAGING_MAX_BYTES": "large"}, "ARTIFACT_STAGING_MAX_BYTES"},
		{"staging smaller than a bundle", map[string]string{"ARTIFACT_STAGING_MAX_BYTES": "1048576"}, "ARTIFACT_STAGING_MAX_BYTES"},
		{"no materialized records", map[string]string{"ARTIFACT_MAX_MAP_RECORDS": "0"}, "ARTIFACT_MAX_MAP_RECORDS"},
		{"unbounded materialized records", map[string]string{"ARTIFACT_MAX_MAP_RECORDS": "100001"}, "ARTIFACT_MAX_MAP_RECORDS"},
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

func TestAlertDeploymentOptions(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
	t.Setenv("GOODSENTRY_DOMAIN", "http://localhost:8000")
	t.Setenv("ALERT_WORKERS", "16")
	t.Setenv("ALERT_MAX_ATTEMPTS", "20")
	t.Setenv("ALERT_DELIVERY_TIMEOUT_SECONDS", "60")
	t.Setenv("GOODSENTRY_WEBHOOK_ALLOW_PRIVATE_IPS", "true")
	c, err := Load()
	if err != nil || c.AlertWorkers != 16 || c.AlertMaxAttempts != 20 || c.AlertTimeoutSeconds != 60 || !c.WebhookAllowPrivate {
		t.Fatalf("alert deployment options were not applied: %v", err)
	}
}

func TestArtifactDeploymentOptions(t *testing.T) {
	for _, tc := range []struct {
		name, ttl, staging, records, body string
		wantTTL, wantRecords              int
		wantStaging                       int64
	}{
		{"defaults", "", "", "", "", 24, 1000, 256 << 20},
		{"minimum policy", "1", "1048576", "1", "1024", 1, 1, 1 << 20},
		{"large deployment", "168", "1099511627776", "100000", "209715200", 168, 100000, 1 << 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
			t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
			for key, value := range map[string]string{"ARTIFACT_CHUNK_TTL_HOURS": tc.ttl, "ARTIFACT_STAGING_MAX_BYTES": tc.staging, "ARTIFACT_MAX_MAP_RECORDS": tc.records, "MAX_BODY_BYTES": tc.body} {
				t.Setenv(key, value)
			}
			c, err := Load()
			if err != nil || c.ArtifactChunkTTLHours != tc.wantTTL || c.ArtifactStagingMaxBytes != tc.wantStaging || c.ArtifactMaxMapRecords != tc.wantRecords {
				t.Fatalf("artifact deployment policy: TTL=%d staging=%d records=%d err=%v", c.ArtifactChunkTTLHours, c.ArtifactStagingMaxBytes, c.ArtifactMaxMapRecords, err)
			}
		})
	}
}
