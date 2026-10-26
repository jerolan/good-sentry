package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These cases protect deployment input and migration aliases before startup.
func TestDeploymentConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		wantError string
	}{
		{"valid", nil, ""},
		{"short secret", map[string]string{"SECRET_KEY": "change_me"}, "SECRET_KEY"},
		{"long placeholder secret", map[string]string{"SECRET_KEY": strings.Repeat("x", 20) + "change_me" + strings.Repeat("y", 20)}, "placeholder"},
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
		{"no transaction retention", map[string]string{"GOODSENTRY_TRANSACTION_RETENTION_DAYS": "0"}, "TRANSACTION_RETENTION_DAYS"},
		{"unbounded legacy transaction retention", map[string]string{"GLITCHTIP_TRANSACTION_RETENTION_DAYS": "3651"}, "TRANSACTION_RETENTION_DAYS"},
		{"fractional transaction retention", map[string]string{"GOODSENTRY_TRANSACTION_RETENTION_DAYS": "1.5"}, "TRANSACTION_RETENTION_DAYS"},
		{"no event retention", map[string]string{"GOODSENTRY_EVENT_RETENTION_DAYS": "0"}, "EVENT_RETENTION_DAYS"},
		{"excessive legacy event retention", map[string]string{"GLITCHTIP_EVENT_RETENTION_DAYS": "3651"}, "EVENT_RETENTION_DAYS"},
		{"fractional event retention", map[string]string{"GOODSENTRY_EVENT_RETENTION_DAYS": "1.5"}, "EVENT_RETENTION_DAYS"},
		{"no uptime retention", map[string]string{"GOODSENTRY_UPTIME_RETENTION_DAYS": "0"}, "UPTIME_RETENTION_DAYS"},
		{"excessive legacy uptime retention", map[string]string{"GLITCHTIP_UPTIME_RETENTION_DAYS": "3651"}, "UPTIME_RETENTION_DAYS"},
		{"fractional uptime retention", map[string]string{"GOODSENTRY_UPTIME_RETENTION_DAYS": "1.5"}, "UPTIME_RETENTION_DAYS"},
		{"no file retention", map[string]string{"GOODSENTRY_FILE_RETENTION_DAYS": "0"}, "FILE_RETENTION_DAYS"},
		{"excessive legacy file retention", map[string]string{"GLITCHTIP_FILE_RETENTION_DAYS": "3651"}, "FILE_RETENTION_DAYS"},
		{"fractional file retention", map[string]string{"GOODSENTRY_FILE_RETENTION_DAYS": "1.5"}, "FILE_RETENTION_DAYS"},
		{"no release retention", map[string]string{"GOODSENTRY_RELEASE_RETENTION_DAYS": "0"}, "RELEASE_RETENTION_DAYS"},
		{"excessive legacy release retention", map[string]string{"GLITCHTIP_RELEASE_RETENTION_DAYS": "3651"}, "RELEASE_RETENTION_DAYS"},
		{"fractional release retention", map[string]string{"GOODSENTRY_RELEASE_RETENTION_DAYS": "1.5"}, "RELEASE_RETENTION_DAYS"},
		{"invalid archive flag", map[string]string{"GOODSENTRY_ENABLE_ARCHIVE": "sometimes"}, "GOODSENTRY_ENABLE_ARCHIVE"},
		{"invalid archive backend", map[string]string{"GOODSENTRY_ARCHIVE_BACKEND": "postgres"}, "GOODSENTRY_ARCHIVE_BACKEND"},
		{"archive threshold is positive", map[string]string{"GOODSENTRY_ARCHIVE_AFTER_DAYS": "0"}, "GOODSENTRY_ARCHIVE_AFTER_DAYS"},
		{"archive threshold cannot outlive spans", map[string]string{"GOODSENTRY_TRANSACTION_RETENTION_DAYS": "7", "GOODSENTRY_ARCHIVE_AFTER_DAYS": "8"}, "GOODSENTRY_ARCHIVE_AFTER_DAYS"},
		{"s3 requires bucket and region", map[string]string{"GOODSENTRY_ENABLE_ARCHIVE": "true", "GOODSENTRY_ARCHIVE_BACKEND": "s3"}, "GOODSENTRY_ARCHIVE_S3_BUCKET"},
		{"archive endpoint must be absolute http", map[string]string{"GOODSENTRY_ARCHIVE_S3_ENDPOINT": "ftp://s3.example.test", "GOODSENTRY_ARCHIVE_BACKEND": "s3", "GOODSENTRY_ENABLE_ARCHIVE": "true", "GOODSENTRY_ARCHIVE_S3_BUCKET": "goodsentry", "GOODSENTRY_ARCHIVE_S3_REGION": "us-east-1"}, "GOODSENTRY_ARCHIVE_S3_ENDPOINT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
			t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
			t.Setenv("GOODSENTRY_DOMAIN", "https://errors.example.com/monitoring/")
			t.Setenv("GLITCHTIP_ENABLE_LOGS", "False")
			t.Setenv("GOODSENTRY_ENABLE_LOGS", "")
			for _, key := range []string{"GOODSENTRY_ENABLE_ARCHIVE", "GOODSENTRY_ENABLE_DUCKDB", "GLITCHTIP_ENABLE_DUCKDB", "GOODSENTRY_ARCHIVE_BACKEND", "GOODSENTRY_ARCHIVE_DIR", "GOODSENTRY_ARCHIVE_AFTER_DAYS", "GOODSENTRY_ARCHIVE_S3_BUCKET", "GOODSENTRY_ARCHIVE_S3_REGION", "GOODSENTRY_ARCHIVE_S3_ENDPOINT", "GOODSENTRY_ARCHIVE_S3_PREFIX", "AWS_REGION"} {
				t.Setenv(key, "")
			}
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

func TestOpenModeGeneratesAndReusesPersistentSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", "")
	t.Setenv("GOODSENTRY_AUTH", "")
	t.Setenv("UPLOAD_DIR", t.TempDir())

	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if first.AuthMode != "disabled" || len(first.Secret) != 64 {
		t.Fatalf("generated secret length = %d; want 64 hex characters", len(first.Secret))
	}
	secretPath := filepath.Join(first.UploadDir, ".goodsentry-secret")
	data, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != first.Secret {
		t.Fatal("persisted secret does not match the loaded key")
	}
	info, err := os.Stat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file permissions = %v; want 0600", info.Mode().Perm())
	}

	second, err := Load()
	if err != nil || second.Secret != first.Secret {
		t.Fatalf("restart did not reuse the persisted secret: %v", err)
	}
}

func TestRequiredAuthNeedsExplicitSecret(t *testing.T) {
	uploadDir := t.TempDir()
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", "")
	t.Setenv("GOODSENTRY_AUTH", "required")
	t.Setenv("UPLOAD_DIR", uploadDir)

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SECRET_KEY is required") {
		t.Fatalf("required auth without a configured key = %v", err)
	}
	if _, err := os.Stat(filepath.Join(uploadDir, ".goodsentry-secret")); !os.IsNotExist(err) {
		t.Fatalf("required auth generated a local key: stat error = %v", err)
	}

	configured := strings.Repeat("b", 64)
	t.Setenv("SECRET_KEY", configured)
	cfg, err := Load()
	if err != nil || cfg.Secret != configured || cfg.AuthMode != "required" {
		t.Fatalf("required auth did not accept the configured key: %v", err)
	}
}

func TestArchiveConfigurationAliasesDefaultsAndRuntimeSupport(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
	t.Setenv("GOODSENTRY_DOMAIN", "http://localhost:8000")
	for _, key := range []string{"GOODSENTRY_ENABLE_ARCHIVE", "GOODSENTRY_ENABLE_DUCKDB", "GLITCHTIP_ENABLE_DUCKDB", "GOODSENTRY_ARCHIVE_BACKEND", "GOODSENTRY_ARCHIVE_DIR", "GOODSENTRY_ARCHIVE_AFTER_DAYS", "GOODSENTRY_ARCHIVE_S3_BUCKET", "GOODSENTRY_ARCHIVE_S3_REGION", "GOODSENTRY_ARCHIVE_S3_ENDPOINT", "GOODSENTRY_ARCHIVE_S3_PREFIX", "AWS_REGION", "GOODSENTRY_TRANSACTION_RETENTION_DAYS"} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ArchiveEnabled || cfg.ArchiveBackend != "local" || cfg.ArchiveDir != "uploads/archive" || cfg.ArchiveAfterDays != 30 {
		t.Fatalf("archive defaults=%+v", cfg)
	}
	if err := cfg.ValidateRuntimeAvailability(); err != nil {
		t.Fatalf("disabled archive affected PostgreSQL-only mode: %v", err)
	}

	t.Setenv("GOODSENTRY_TRANSACTION_RETENTION_DAYS", "20")
	cfg, err = Load()
	if err != nil || cfg.ArchiveAfterDays != 10 {
		t.Fatalf("derived archive threshold=%d err=%v", cfg.ArchiveAfterDays, err)
	}
	t.Setenv("GOODSENTRY_ARCHIVE_AFTER_DAYS", "7")
	cfg, err = Load()
	if err != nil || cfg.ArchiveAfterDays != 7 {
		t.Fatalf("explicit archive threshold=%d err=%v", cfg.ArchiveAfterDays, err)
	}

	t.Setenv("GLITCHTIP_ENABLE_DUCKDB", "true")
	cfg, err = Load()
	if err != nil || !cfg.ArchiveEnabled {
		t.Fatalf("GlitchTip archive alias=%v err=%v", cfg.ArchiveEnabled, err)
	}
	t.Setenv("GOODSENTRY_ENABLE_DUCKDB", "false")
	cfg, err = Load()
	if err != nil || cfg.ArchiveEnabled {
		t.Fatalf("GoodSentry compatibility alias precedence=%v err=%v", cfg.ArchiveEnabled, err)
	}
	t.Setenv("GOODSENTRY_ENABLE_ARCHIVE", "true")
	cfg, err = Load()
	if err != nil || !cfg.ArchiveEnabled {
		t.Fatalf("canonical archive flag precedence=%v err=%v", cfg.ArchiveEnabled, err)
	}
	if err := cfg.ValidateRuntimeAvailability(); err != nil {
		t.Fatalf("implemented archive was rejected by the runtime gate: %v", err)
	}

	t.Setenv("GOODSENTRY_ARCHIVE_BACKEND", "s3")
	t.Setenv("GOODSENTRY_ARCHIVE_S3_BUCKET", "goodsentry-archive")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("GOODSENTRY_ARCHIVE_S3_ENDPOINT", "http://minio.example.test:9000")
	t.Setenv("GOODSENTRY_ARCHIVE_S3_PREFIX", "/telemetry/archive/")
	cfg, err = Load()
	if err != nil || cfg.ArchiveBackend != "s3" || cfg.ArchiveS3Region != "us-east-1" || cfg.ArchiveS3Prefix != "telemetry/archive" {
		t.Fatalf("S3 archive configuration=%+v err=%v", cfg, err)
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

func TestAuthenticationModeDefaultsAndAdmission(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
	t.Setenv("GOODSENTRY_DOMAIN", "http://localhost:8000")
	for _, key := range []string{"GOODSENTRY_AUTH", "GOODSENTRY_DEFAULT_EMAIL", "GOODSENTRY_DEFAULT_NAME", "GOODSENTRY_DEFAULT_ORGANIZATION", "GOODSENTRY_DEFAULT_PASSWORD"} {
		t.Setenv(key, "")
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthMode != "disabled" || !c.AuthenticationDisabled() || c.DefaultEmail != "owner@localhost" || c.DefaultName != "GoodSentry Owner" || c.DefaultOrganization != "Default Organization" || c.DefaultPassword != "" {
		t.Fatalf("open authentication defaults=%+v", c)
	}
	t.Setenv("GOODSENTRY_AUTH", "required")
	t.Setenv("GOODSENTRY_DEFAULT_EMAIL", "admin@example.test")
	t.Setenv("GOODSENTRY_DEFAULT_NAME", "Implicit Admin")
	t.Setenv("GOODSENTRY_DEFAULT_ORGANIZATION", "Implicit Workspace")
	t.Setenv("GOODSENTRY_DEFAULT_PASSWORD", "open-mode-password")
	c, err = Load()
	if err != nil || c.AuthMode != "required" || c.AuthenticationDisabled() || c.DefaultEmail != "admin@example.test" || c.DefaultName != "Implicit Admin" || c.DefaultOrganization != "Implicit Workspace" || c.DefaultPassword != "open-mode-password" {
		t.Fatalf("required authentication config=%+v err=%v", c, err)
	}
	for _, tc := range []struct{ key, value, want string }{
		{"GOODSENTRY_AUTH", "optional", "GOODSENTRY_AUTH"},
		{"GOODSENTRY_DEFAULT_EMAIL", "not-an-email", "DEFAULT_EMAIL"},
		{"GOODSENTRY_DEFAULT_NAME", " ", "DEFAULT_NAME"},
		{"GOODSENTRY_DEFAULT_ORGANIZATION", " ", "DEFAULT_ORGANIZATION"},
		{"GOODSENTRY_DEFAULT_PASSWORD", "short", "DEFAULT_PASSWORD"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want=%s", err, tc.want)
			}
		})
	}
}

func TestFeatureAliasesAndRuntimeOptions(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":                 "postgres://goodsentry:local@localhost/goodsentry",
		"SECRET_KEY":                   strings.Repeat("a", 64),
		"GOODSENTRY_DOMAIN":            "http://localhost:8000",
		"SERVER_ROLE":                  "all_in_one",
		"VALKEY_URL":                   "",
		"GLITCHTIP_ENABLE_LOGS":        "false",
		"GLITCHTIP_ENABLE_UPTIME":      "false",
		"GLITCHTIP_ENABLE_MCP":         "true",
		"GLITCHTIP_ENABLE_DUCKDB":      "true",
		"GOODSENTRY_ENABLE_MCP":        "false",
		"GOODSENTRY_ENABLE_ARCHIVE":    "",
		"GOODSENTRY_ENABLE_DUCKDB":     "false",
		"ENABLE_ORGANIZATION_CREATION": "false",
		"ENABLE_ADMIN":                 "false",
		"ENABLE_OPENAPI":               "false",
	}
	for key, value := range base {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Logs || cfg.Uptime || cfg.MCP || cfg.ArchiveEnabled || cfg.Admin || cfg.OpenAPI || cfg.OrganizationCreation || cfg.ValkeyURL != "" {
		t.Fatalf("alias precedence/runtime flags=%+v", cfg)
	}
	if err := cfg.ValidateRuntimeAvailability(); err != nil {
		t.Fatalf("PostgreSQL-only defaults unavailable: %v", err)
	}
	for _, key := range []string{"ENABLE_ADMIN", "ENABLE_OPENAPI"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "true")
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.ValidateRuntimeAvailability(); err != nil {
				t.Fatalf("implemented %s rejected: %v", key, err)
			}
			if key == "ENABLE_ADMIN" && !cfg.Admin || key == "ENABLE_OPENAPI" && !cfg.OpenAPI {
				t.Fatalf("%s was not retained: %+v", key, cfg)
			}
		})
	}
	t.Setenv("GOODSENTRY_ENABLE_MCP", "true")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateRuntimeAvailability(); err != nil {
		t.Fatalf("implemented MCP rejected: %v", err)
	}
	t.Setenv("ENABLE_ORGANIZATION_CREATION", "true")
	cfg, err = Load()
	if err != nil || !cfg.OrganizationCreation {
		t.Fatalf("organization creation flag not loaded: %+v %v", cfg, err)
	}
	t.Setenv("GOODSENTRY_ENABLE_ORGANIZATION_CREATION", "false")
	cfg, err = Load()
	if err != nil || cfg.OrganizationCreation {
		t.Fatalf("organization creation precedence: %+v %v", cfg, err)
	}
	t.Setenv("GOODSENTRY_ENABLE_MCP", "false")
	t.Setenv("GOODSENTRY_ENABLE_DUCKDB", "false")
	t.Setenv("ENABLE_ADMIN", "false")
	t.Setenv("ENABLE_OPENAPI", "false")
	t.Setenv("VALKEY_URL", "redis://valkey.invalid:6379")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateRuntimeAvailability(); err == nil || !strings.Contains(err.Error(), "Valkey") {
		t.Fatalf("Valkey mode accepted: %v", err)
	}
	t.Setenv("VALKEY_URL", "")
	t.Setenv("SERVER_ROLE", "worker")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SERVER_ROLE") {
		t.Fatalf("unsupported role accepted: %v", err)
	}
}

func TestDomainAliasPrecedence(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
	t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
	t.Setenv("GLITCHTIP_DOMAIN", "https://legacy.example.test/monitoring/")
	t.Setenv("GOODSENTRY_DOMAIN", "")

	legacy, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Domain != "https://legacy.example.test/monitoring" || legacy.Prefix != "/monitoring" {
		t.Fatalf("legacy domain = %q prefix = %q", legacy.Domain, legacy.Prefix)
	}

	t.Setenv("GOODSENTRY_DOMAIN", "https://current.example.test/app/")
	current, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if current.Domain != "https://current.example.test/app" || current.Prefix != "/app" {
		t.Fatalf("current domain = %q prefix = %q", current.Domain, current.Prefix)
	}
}

// The deployment loader owns precedence and integer admission. These cases
// catch ignored aliases, stale inherited lifetimes, or a lower-priority
// invalid value overriding an explicit setting. No runtime cleanup is claimed.
func TestTelemetryRetentionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name                                                                  string
		env                                                                   map[string]string
		master, logs, transactions, events, uptime, files, releases, interval int
	}{
		{"defaults", nil, 90, 90, 90, 90, 90, 90, 365, 60},
		{"existing master inherits", map[string]string{"RETENTION_DAYS": "15"}, 15, 15, 15, 15, 15, 15, 365, 60},
		{"legacy master inherits", map[string]string{"GLITCHTIP_RETENTION_DAYS": "30", "RETENTION_DAYS": "bad"}, 30, 30, 30, 30, 30, 30, 365, 60},
		{"explicit master wins", map[string]string{"GOODSENTRY_RETENTION_DAYS": "40", "GLITCHTIP_RETENTION_DAYS": "bad", "RETENTION_DAYS": "bad"}, 40, 40, 40, 40, 40, 40, 365, 60},
		{"legacy log override", map[string]string{"GLITCHTIP_LOG_RETENTION_DAYS": "7"}, 90, 7, 90, 90, 90, 90, 365, 60},
		{"explicit log wins", map[string]string{"GOODSENTRY_LOG_RETENTION_DAYS": "8", "GLITCHTIP_LOG_RETENTION_DAYS": "bad"}, 90, 8, 90, 90, 90, 90, 365, 60},
		{"legacy transaction override", map[string]string{"GLITCHTIP_TRANSACTION_RETENTION_DAYS": "7"}, 90, 90, 7, 90, 90, 90, 365, 60},
		{"explicit transaction wins", map[string]string{"GOODSENTRY_TRANSACTION_RETENTION_DAYS": "8", "GLITCHTIP_TRANSACTION_RETENTION_DAYS": "bad"}, 90, 90, 8, 90, 90, 90, 365, 60},
		{"legacy event override", map[string]string{"GLITCHTIP_EVENT_RETENTION_DAYS": "7"}, 90, 90, 90, 7, 90, 90, 365, 60},
		{"explicit event wins", map[string]string{"GOODSENTRY_EVENT_RETENTION_DAYS": "8", "GLITCHTIP_EVENT_RETENTION_DAYS": "bad"}, 90, 90, 90, 8, 90, 90, 365, 60},
		{"legacy uptime override", map[string]string{"GLITCHTIP_UPTIME_RETENTION_DAYS": "7"}, 90, 90, 90, 90, 7, 90, 365, 60},
		{"explicit uptime wins", map[string]string{"GOODSENTRY_UPTIME_RETENTION_DAYS": "8", "GLITCHTIP_UPTIME_RETENTION_DAYS": "bad"}, 90, 90, 90, 90, 8, 90, 365, 60},
		{"legacy file override", map[string]string{"GLITCHTIP_FILE_RETENTION_DAYS": "7"}, 90, 90, 90, 90, 90, 7, 365, 60},
		{"explicit file wins", map[string]string{"GOODSENTRY_FILE_RETENTION_DAYS": "8", "GLITCHTIP_FILE_RETENTION_DAYS": "bad"}, 90, 90, 90, 90, 90, 8, 365, 60},
		{"legacy release override", map[string]string{"GLITCHTIP_RELEASE_RETENTION_DAYS": "7"}, 90, 90, 90, 90, 90, 90, 7, 60},
		{"explicit release wins", map[string]string{"GOODSENTRY_RELEASE_RETENTION_DAYS": "8", "GLITCHTIP_RELEASE_RETENTION_DAYS": "bad"}, 90, 90, 90, 90, 90, 90, 8, 60},
		{"empty explicit values fall through", map[string]string{"GOODSENTRY_RETENTION_DAYS": "45", "GOODSENTRY_FILE_RETENTION_DAYS": "", "GLITCHTIP_FILE_RETENTION_DAYS": "7", "GOODSENTRY_RELEASE_RETENTION_DAYS": "", "GLITCHTIP_RELEASE_RETENTION_DAYS": "8"}, 45, 45, 45, 45, 45, 7, 8, 60},
		{"independent classes", map[string]string{"GOODSENTRY_RETENTION_DAYS": "40", "GOODSENTRY_LOG_RETENTION_DAYS": "7", "GOODSENTRY_TRANSACTION_RETENTION_DAYS": "20", "GOODSENTRY_EVENT_RETENTION_DAYS": "25", "GOODSENTRY_UPTIME_RETENTION_DAYS": "30", "GOODSENTRY_FILE_RETENTION_DAYS": "35", "GOODSENTRY_RELEASE_RETENTION_DAYS": "50"}, 40, 7, 20, 25, 30, 35, 50, 60},
		{"minimum", map[string]string{"GOODSENTRY_RETENTION_DAYS": "1", "GOODSENTRY_LOG_RETENTION_DAYS": "1", "GOODSENTRY_TRANSACTION_RETENTION_DAYS": "1", "GOODSENTRY_EVENT_RETENTION_DAYS": "1", "GOODSENTRY_UPTIME_RETENTION_DAYS": "1", "GOODSENTRY_FILE_RETENTION_DAYS": "1", "GOODSENTRY_RELEASE_RETENTION_DAYS": "1", "GOODSENTRY_RETENTION_INTERVAL_SECONDS": "1"}, 1, 1, 1, 1, 1, 1, 1, 1},
		{"maximum", map[string]string{"GOODSENTRY_RETENTION_DAYS": "3650", "GOODSENTRY_LOG_RETENTION_DAYS": "3650", "GOODSENTRY_TRANSACTION_RETENTION_DAYS": "3650", "GOODSENTRY_EVENT_RETENTION_DAYS": "3650", "GOODSENTRY_UPTIME_RETENTION_DAYS": "3650", "GOODSENTRY_FILE_RETENTION_DAYS": "3650", "GOODSENTRY_RELEASE_RETENTION_DAYS": "3650", "GOODSENTRY_RETENTION_INTERVAL_SECONDS": "3600"}, 3650, 3650, 3650, 3650, 3650, 3650, 3650, 3600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://goodsentry:local@localhost/goodsentry")
			t.Setenv("SECRET_KEY", strings.Repeat("a", 64))
			for _, key := range []string{"GOODSENTRY_RETENTION_DAYS", "GLITCHTIP_RETENTION_DAYS", "RETENTION_DAYS", "GOODSENTRY_LOG_RETENTION_DAYS", "GLITCHTIP_LOG_RETENTION_DAYS", "GOODSENTRY_TRANSACTION_RETENTION_DAYS", "GLITCHTIP_TRANSACTION_RETENTION_DAYS", "GOODSENTRY_EVENT_RETENTION_DAYS", "GLITCHTIP_EVENT_RETENTION_DAYS", "GOODSENTRY_UPTIME_RETENTION_DAYS", "GLITCHTIP_UPTIME_RETENTION_DAYS", "GOODSENTRY_FILE_RETENTION_DAYS", "GLITCHTIP_FILE_RETENTION_DAYS", "GOODSENTRY_RELEASE_RETENTION_DAYS", "GLITCHTIP_RELEASE_RETENTION_DAYS", "GOODSENTRY_RETENTION_INTERVAL_SECONDS"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RetentionDays != tc.master || cfg.LogRetentionDays != tc.logs || cfg.TransactionRetentionDays != tc.transactions || cfg.EventRetentionDays != tc.events || cfg.UptimeRetentionDays != tc.uptime || cfg.FileRetentionDays != tc.files || cfg.ReleaseRetentionDays != tc.releases || cfg.RetentionIntervalSeconds != tc.interval {
				t.Fatalf("master/logs/transactions/events/uptime/files/releases/interval=%d/%d/%d/%d/%d/%d/%d/%d, want %d/%d/%d/%d/%d/%d/%d/%d", cfg.RetentionDays, cfg.LogRetentionDays, cfg.TransactionRetentionDays, cfg.EventRetentionDays, cfg.UptimeRetentionDays, cfg.FileRetentionDays, cfg.ReleaseRetentionDays, cfg.RetentionIntervalSeconds, tc.master, tc.logs, tc.transactions, tc.events, tc.uptime, tc.files, tc.releases, tc.interval)
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

func TestOIDCConfiguration(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":      "postgres://goodsentry:local@localhost/goodsentry",
		"SECRET_KEY":        strings.Repeat("a", 64),
		"GOODSENTRY_DOMAIN": "https://errors.example.test",
	}
	for _, tc := range []struct {
		name      string
		env       map[string]string
		wantError string
	}{
		{"disabled", nil, ""},
		{"valid", map[string]string{
			"GOODSENTRY_OIDC_ISSUER": "https://id.example.test/tenant/", "GOODSENTRY_OIDC_CLIENT_ID": "client",
			"GOODSENTRY_OIDC_CLIENT_SECRET": "secret", "GOODSENTRY_OIDC_NAME": "Company SSO",
			"GOODSENTRY_OIDC_SCOPES": "openid profile email groups", "GOODSENTRY_OIDC_AUTO_CREATE": "true",
		}, ""},
		{"loopback HTTP", map[string]string{
			"GOODSENTRY_OIDC_ISSUER": "http://127.0.0.1:9000", "GOODSENTRY_OIDC_CLIENT_ID": "client",
			"GOODSENTRY_OIDC_CLIENT_SECRET": "secret",
		}, ""},
		{"remote HTTP", map[string]string{
			"GOODSENTRY_OIDC_ISSUER": "http://id.example.test", "GOODSENTRY_OIDC_CLIENT_ID": "client",
			"GOODSENTRY_OIDC_CLIENT_SECRET": "secret",
		}, "OIDC_ISSUER"},
		{"missing secret", map[string]string{
			"GOODSENTRY_OIDC_ISSUER": "https://id.example.test", "GOODSENTRY_OIDC_CLIENT_ID": "client",
		}, "CLIENT_SECRET"},
		{"missing openid", map[string]string{
			"GOODSENTRY_OIDC_ISSUER": "https://id.example.test", "GOODSENTRY_OIDC_CLIENT_ID": "client",
			"GOODSENTRY_OIDC_CLIENT_SECRET": "secret", "GOODSENTRY_OIDC_SCOPES": "profile email",
		}, "include openid"},
		{"bad scope", map[string]string{
			"GOODSENTRY_OIDC_ISSUER": "https://id.example.test", "GOODSENTRY_OIDC_CLIENT_ID": "client",
			"GOODSENTRY_OIDC_CLIENT_SECRET": "secret", "GOODSENTRY_OIDC_SCOPES": "openid bad/scope",
		}, "invalid scope"},
		{"bad auto create", map[string]string{
			"GOODSENTRY_OIDC_ISSUER": "https://id.example.test", "GOODSENTRY_OIDC_CLIENT_ID": "client",
			"GOODSENTRY_OIDC_CLIENT_SECRET": "secret", "GOODSENTRY_OIDC_AUTO_CREATE": "sometimes",
		}, "AUTO_CREATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range base {
				t.Setenv(key, value)
			}
			for _, key := range []string{"GOODSENTRY_OIDC_ISSUER", "GOODSENTRY_OIDC_CLIENT_ID", "GOODSENTRY_OIDC_CLIENT_SECRET", "GOODSENTRY_OIDC_NAME", "GOODSENTRY_OIDC_SCOPES", "GOODSENTRY_OIDC_AUTO_CREATE"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			c, err := Load()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error=%v want %s", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "valid" {
				if c.OIDCIssuer != "https://id.example.test/tenant" || c.OIDCClientID != "client" || c.OIDCName != "Company SSO" ||
					!c.OIDCAutoCreate || strings.Join(c.OIDCScopes, " ") != "openid profile email groups" {
					t.Fatalf("OIDC config=%+v", c)
				}
			}
		})
	}
}
