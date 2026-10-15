package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"goodsentry/internal/notify"
)

type Config struct {
	DatabaseURL, Secret, Domain, Prefix, Addr string
	EmailURL, FromEmail, ValkeyURL, UploadDir string
	Logs, Uptime, MCP, DuckDB, OpenAPI, Admin bool
	MaxBodyBytes                              int64
	RetentionDays                             int
	MonitorWorkers                            int
	UptimeAllowPrivate                        bool
	AlertWorkers, AlertMaxAttempts            int
	AlertTimeoutSeconds                       int
	WebhookAllowPrivate                       bool
	AlertDelivery                             bool
	ArtifactChunkTTLHours                     int
	ArtifactStagingMaxBytes                   int64
	ArtifactMaxMapRecords                     int
}

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Secret: os.Getenv("SECRET_KEY"), EmailURL: value("EMAIL_URL", "consolemail://"), FromEmail: value("DEFAULT_FROM_EMAIL", "goodsentry@localhost"), ValkeyURL: os.Getenv("VALKEY_URL"), UploadDir: value("UPLOAD_DIR", "uploads"), MaxBodyBytes: 32 << 20, RetentionDays: 90, MonitorWorkers: 2, AlertWorkers: 2, AlertMaxAttempts: 5, AlertTimeoutSeconds: 10}
	c.ArtifactChunkTTLHours, c.ArtifactStagingMaxBytes, c.ArtifactMaxMapRecords = 24, 256<<20, 1000
	port, err := strconv.Atoi(value("PORT", "8000"))
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("PORT must be an integer from 1 to 65535")
	}
	c.Addr = ":" + strconv.Itoa(port)
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return c, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL")
	}
	if len(c.Secret) < 32 || strings.Contains(strings.ToLower(c.Secret), "change_me") {
		return c, fmt.Errorf("SECRET_KEY must contain at least 32 characters and cannot be a placeholder")
	}
	c.Domain = strings.TrimRight(alias("GOODSENTRY_DOMAIN", "GLITCHTIP_DOMAIN", "http://localhost:"+strconv.Itoa(port)), "/")
	u, err = url.Parse(c.Domain)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("GOODSENTRY_DOMAIN must be an absolute http or https URL without credentials, query or fragment")
	}
	c.Prefix = strings.TrimRight(u.Path, "/")
	if role := value("SERVER_ROLE", "all_in_one"); role != "all_in_one" {
		return c, fmt.Errorf("SERVER_ROLE must be all_in_one")
	}
	for _, flag := range []struct {
		name, legacy string
		def          bool
		dest         *bool
	}{
		{"GOODSENTRY_ENABLE_LOGS", "GLITCHTIP_ENABLE_LOGS", true, &c.Logs},
		{"GOODSENTRY_ENABLE_UPTIME", "GLITCHTIP_ENABLE_UPTIME", true, &c.Uptime},
		{"GOODSENTRY_UPTIME_ALLOW_PRIVATE_IPS", "GLITCHTIP_UPTIME_ALLOW_PRIVATE_IPS", false, &c.UptimeAllowPrivate},
		{"GOODSENTRY_WEBHOOK_ALLOW_PRIVATE_IPS", "", false, &c.WebhookAllowPrivate},
		{"GOODSENTRY_ENABLE_MCP", "GLITCHTIP_ENABLE_MCP", false, &c.MCP},
		{"GOODSENTRY_ENABLE_DUCKDB", "GLITCHTIP_ENABLE_DUCKDB", false, &c.DuckDB},
		{"ENABLE_OPENAPI", "", false, &c.OpenAPI}, {"ENABLE_ADMIN", "", false, &c.Admin},
	} {
		v, err := strconv.ParseBool(alias(flag.name, flag.legacy, strconv.FormatBool(flag.def)))
		if err != nil {
			return c, fmt.Errorf("%s must be true or false", flag.name)
		}
		*flag.dest = v
	}
	for _, option := range []struct {
		name string
		max  int
		dest *int
	}{
		{"ALERT_WORKERS", 16, &c.AlertWorkers},
		{"ALERT_MAX_ATTEMPTS", 20, &c.AlertMaxAttempts},
		{"ALERT_DELIVERY_TIMEOUT_SECONDS", 60, &c.AlertTimeoutSeconds},
		{"ARTIFACT_CHUNK_TTL_HOURS", 168, &c.ArtifactChunkTTLHours},
		{"ARTIFACT_MAX_MAP_RECORDS", 100000, &c.ArtifactMaxMapRecords},
	} {
		if raw := os.Getenv(option.name); raw != "" {
			*option.dest, err = strconv.Atoi(raw)
			if err != nil || *option.dest < 1 || *option.dest > option.max {
				return c, fmt.Errorf("%s must be between 1 and %d", option.name, option.max)
			}
		}
	}
	if err := notify.ValidateMail(c.EmailURL, c.FromEmail); err != nil {
		return c, fmt.Errorf("EMAIL_URL or DEFAULT_FROM_EMAIL: %w", err)
	}
	if raw := os.Getenv("MONITOR_WORKERS"); raw != "" {
		c.MonitorWorkers, err = strconv.Atoi(raw)
		if err != nil || c.MonitorWorkers < 1 || c.MonitorWorkers > 16 {
			return c, fmt.Errorf("MONITOR_WORKERS must be between 1 and 16")
		}
	}
	if raw := os.Getenv("MAX_BODY_BYTES"); raw != "" {
		c.MaxBodyBytes, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || c.MaxBodyBytes < 1024 || c.MaxBodyBytes > 200<<20 {
			return c, fmt.Errorf("MAX_BODY_BYTES must be between 1024 and 209715200")
		}
	}
	if raw := os.Getenv("RETENTION_DAYS"); raw != "" {
		c.RetentionDays, err = strconv.Atoi(raw)
		if err != nil || c.RetentionDays < 1 || c.RetentionDays > 3650 {
			return c, fmt.Errorf("RETENTION_DAYS must be between 1 and 3650")
		}
	}
	if raw := os.Getenv("ARTIFACT_STAGING_MAX_BYTES"); raw != "" {
		c.ArtifactStagingMaxBytes, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return c, fmt.Errorf("ARTIFACT_STAGING_MAX_BYTES must be an integer byte count")
		}
	}
	if c.ArtifactStagingMaxBytes < 1<<20 || c.ArtifactStagingMaxBytes > 1<<40 || c.ArtifactStagingMaxBytes < c.MaxBodyBytes {
		return c, fmt.Errorf("ARTIFACT_STAGING_MAX_BYTES must be between 1048576 and 1099511627776 and at least MAX_BODY_BYTES")
	}
	return c, nil
}

func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func alias(name, legacy, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if legacy != "" {
		if v := os.Getenv(legacy); v != "" {
			return v
		}
	}
	return fallback
}
