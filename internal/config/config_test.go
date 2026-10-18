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
		{"no issue maintenance", map[string]string{"ISSUE_MAINTENANCE_INTERVAL_SECONDS": "0"}, "ISSUE_MAINTENANCE_INTERVAL_SECONDS"},
		{"unbounded issue maintenance delay", map[string]string{"ISSUE_MAINTENANCE_INTERVAL_SECONDS": "3601"}, "ISSUE_MAINTENANCE_INTERVAL_SECONDS"},
		{"fractional issue maintenance delay", map[string]string{"ISSUE_MAINTENANCE_INTERVAL_SECONDS": "1.5"}, "ISSUE_MAINTENANCE_INTERVAL_SECONDS"},
		{"otlp no records", map[string]string{"GOODSENTRY_OTLP_MAX_RECORDS": "0"}, "GOODSENTRY_OTLP_MAX_RECORDS"},
		{"otlp unbounded records", map[string]string{"GOODSENTRY_OTLP_MAX_RECORDS": "100001"}, "GOODSENTRY_OTLP_MAX_RECORDS"},
		{"otlp fractional records", map[string]string{"GOODSENTRY_OTLP_MAX_RECORDS": "1.5"}, "GOODSENTRY_OTLP_MAX_RECORDS"},
		{"otlp too few nodes", map[string]string{"GOODSENTRY_OTLP_MAX_NODES": "99"}, "GOODSENTRY_OTLP_MAX_NODES"},
		{"otlp unbounded nodes", map[string]string{"GOODSENTRY_OTLP_MAX_NODES": "1000001"}, "GOODSENTRY_OTLP_MAX_NODES"},
		{"otlp malformed nodes", map[string]string{"GOODSENTRY_OTLP_MAX_NODES": "many"}, "GOODSENTRY_OTLP_MAX_NODES"},
		{"invalid retention master", map[string]string{"GOODSENTRY_RETENTION_DAYS": "many"}, "GOODSENTRY_RETENTION_DAYS"},
		{"invalid legacy retention master", map[string]string{"GLITCHTIP_RETENTION_DAYS": "0"}, "RETENTION_DAYS"},
		{"invalid log retention", map[string]string{"GOODSENTRY_LOG_RETENTION_DAYS": "0"}, "LOG_RETENTION_DAYS"},
		{"invalid legacy log retention", map[string]string{"GLITCHTIP_LOG_RETENTION_DAYS": "3651"}, "LOG_RETENTION_DAYS"},
		{"fractional log retention", map[string]string{"GOODSENTRY_LOG_RETENTION_DAYS": "1.5"}, "LOG_RETENTION_DAYS"},
		{"invalid retention interval", map[string]string{"GOODSENTRY_RETENTION_INTERVAL_SECONDS": "0"}, "RETENTION_INTERVAL_SECONDS"},
		{"unbounded retention interval", map[string]string{"GOODSENTRY_RETENTION_INTERVAL_SECONDS": "3601"}, "RETENTION_INTERVAL_SECONDS"},
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

// The deployment loader owns precedence and integer admission. These cases
// catch ignored aliases, a stale inherited log lifetime, or a lower-priority
// invalid value overriding an explicit setting. No runtime cleanup is claimed.
func TestLogRetentionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		env                    map[string]string
		master, logs, interval int
	}{
		{"defaults", nil, 90, 90, 60},
		{"existing master inherits", map[string]string{"RETENTION_DAYS": "15"}, 15, 15, 60},
		{"legacy master inherits", map[string]string{"GLITCHTIP_RETENTION_DAYS": "30", "RETENTION_DAYS": "bad"}, 30, 30, 60},
		{"explicit master wins", map[string]string{"GOODSENTRY_RETENTION_DAYS": "40", "GLITCHTIP_RETENTION_DAYS": "bad", "RETENTION_DAYS": "bad"}, 40, 40, 60},
		{"legacy log override", map[string]string{"GLITCHTIP_LOG_RETENTION_DAYS": "7"}, 90, 7, 60},
		{"explicit log wins", map[string]string{"GOODSENTRY_LOG_RETENTION_DAYS": "8", "GLITCHTIP_LOG_RETENTION_DAYS": "bad"}, 90, 8, 60},
		{"minimum", map[string]string{"GOODSENTRY_RETENTION_DAYS": "1", "GOODSENTRY_LOG_RETENTION_DAYS": "1", "GOODSENTRY_RETENTION_INTERVAL_SECONDS": "1"}, 1, 1, 1},
		{"maximum", map[string]string{"GOODSENTRY_RETENTION_DAYS": "3650", "GOODSENTRY_LOG_RETENTION_DAYS": "3650", "GOODSENTRY_RETENTION_INTERVAL_SECONDS": "3600"}, 3650, 3650, 3600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
			t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
			for _, key := range []string{"GOODSENTRY_RETENTION_DAYS", "GLITCHTIP_RETENTION_DAYS", "RETENTION_DAYS", "GOODSENTRY_LOG_RETENTION_DAYS", "GLITCHTIP_LOG_RETENTION_DAYS", "GOODSENTRY_RETENTION_INTERVAL_SECONDS"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RetentionDays != tc.master || cfg.LogRetentionDays != tc.logs || cfg.RetentionIntervalSeconds != tc.interval {
				t.Fatalf("master/logs/interval=%d/%d/%d, want %d/%d/%d", cfg.RetentionDays, cfg.LogRetentionDays, cfg.RetentionIntervalSeconds, tc.master, tc.logs, tc.interval)
			}
		})
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

// Deployment parsing owns default/override/range admission. The worker consumes
// typed durations and cannot detect a silently ignored environment variable.
// The cases use the production Load boundary without clock or scheduler seams.
func TestIssueMaintenanceDeploymentOptions(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
	for _, tc := range []struct {
		raw  string
		want int
	}{{"", 1}, {"1", 1}, {"30", 30}, {"3600", 3600}} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("ISSUE_MAINTENANCE_INTERVAL_SECONDS", tc.raw)
			c, err := Load()
			if err != nil || c.IssueMaintenanceIntervalSeconds != tc.want {
				t.Fatalf("issue maintenance interval=%d want=%d err=%v", c.IssueMaintenanceIntervalSeconds, tc.want, err)
			}
		})
	}
}

func TestOTLPDeploymentOptions(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
	for _, tc := range []struct {
		name, records, nodes   string
		wantRecords, wantNodes int
	}{
		{"defaults", "", "", 1000, 100000},
		{"minimum", "1", "100", 1, 100},
		{"maximum", "100000", "1000000", 100000, 1000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOODSENTRY_OTLP_MAX_RECORDS", tc.records)
			t.Setenv("GOODSENTRY_OTLP_MAX_NODES", tc.nodes)
			c, err := Load()
			if err != nil || c.OTLPMaxRecords != tc.wantRecords || c.OTLPMaxNodes != tc.wantNodes {
				t.Fatalf("OTLP records=%d nodes=%d want=%d/%d err=%v", c.OTLPMaxRecords, c.OTLPMaxNodes, tc.wantRecords, tc.wantNodes, err)
			}
		})
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
