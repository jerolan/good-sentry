package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"goodsentry/internal/notify"
)

type Config struct {
	DatabaseURL, Secret, Domain, Prefix, Addr                    string
	EmailURL, FromEmail, ValkeyURL, UploadDir                    string
	AuthMode                                                     string
	DefaultEmail, DefaultName                                    string
	DefaultOrganization, DefaultPassword                         string
	Logs, Uptime, MCP, ArchiveEnabled, OpenAPI, Admin            bool
	ArchiveBackend, ArchiveDir, ArchiveS3Bucket, ArchiveS3Region string
	ArchiveS3Endpoint, ArchiveS3Prefix                           string
	ArchiveAfterDays                                             int
	OrganizationCreation                                         bool
	MaxBodyBytes                                                 int64
	RetentionDays, LogRetentionDays                              int
	TransactionRetentionDays                                     int
	EventRetentionDays                                           int
	UptimeRetentionDays                                          int
	FileRetentionDays, ReleaseRetentionDays                      int
	RetentionIntervalSeconds                                     int
	MonitorWorkers                                               int
	UptimeAllowPrivate                                           bool
	DebugInfodURLs                                               []string
	DebugInfodAllowPrivate                                       bool
	AlertWorkers, AlertMaxAttempts                               int
	AlertTimeoutSeconds                                          int
	WebhookAllowPrivate                                          bool
	AlertDelivery                                                bool
	ArtifactChunkTTLHours                                        int
	ArtifactStagingMaxBytes                                      int64
	ArtifactMaxMapRecords                                        int
	IssueMaintenanceIntervalSeconds                              int
	OTLPMaxRecords, OTLPMaxNodes                                 int
	OIDCIssuer, OIDCClientID, OIDCClientSecret                   string
	OIDCName                                                     string
	OIDCScopes                                                   []string
	OIDCAutoCreate                                               bool
}

func (c Config) AuthenticationDisabled() bool { return c.AuthMode == "disabled" }

// ValidateRuntimeAvailability rejects external services not supported by this
// build. Parsing a flag is not permission to claim support.
func (c Config) ValidateRuntimeAvailability() error {
	if c.ValkeyURL != "" {
		return fmt.Errorf("Valkey is not implemented; leave VALKEY_URL empty for PostgreSQL-only mode")
	}
	return nil
}

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Secret: os.Getenv("SECRET_KEY"), EmailURL: value("EMAIL_URL", "consolemail://"), FromEmail: value("DEFAULT_FROM_EMAIL", "goodsentry@localhost"), ValkeyURL: os.Getenv("VALKEY_URL"), UploadDir: value("UPLOAD_DIR", "uploads"), MaxBodyBytes: 32 << 20, RetentionDays: 90, MonitorWorkers: 2, AlertWorkers: 2, AlertMaxAttempts: 5, AlertTimeoutSeconds: 10}
	c.ArtifactChunkTTLHours, c.ArtifactStagingMaxBytes, c.ArtifactMaxMapRecords = 24, 256<<20, 1000
	c.IssueMaintenanceIntervalSeconds = 1
	c.RetentionIntervalSeconds = 60
	c.OTLPMaxRecords, c.OTLPMaxNodes = 1000, 100000
	port, err := strconv.Atoi(value("PORT", "8000"))
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("PORT must be an integer from 1 to 65535")
	}
	c.Addr = ":" + strconv.Itoa(port)
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return c, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL")
	}
	c.AuthMode = strings.ToLower(strings.TrimSpace(value("GOODSENTRY_AUTH", "disabled")))
	if c.AuthMode != "disabled" && c.AuthMode != "required" {
		return c, fmt.Errorf("GOODSENTRY_AUTH must be disabled or required")
	}
	c.Domain = strings.TrimRight(alias("GOODSENTRY_DOMAIN", "GLITCHTIP_DOMAIN", "http://localhost:"+strconv.Itoa(port)), "/")
	u, err = url.Parse(c.Domain)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("GOODSENTRY_DOMAIN must be an absolute http or https URL without credentials, query or fragment")
	}
	c.Prefix = strings.TrimRight(u.Path, "/")
	c.DebugInfodURLs = strings.Fields(os.Getenv("GOODSENTRY_DEBUGINFOD_URLS"))
	if len(c.DebugInfodURLs) > 4 {
		return c, fmt.Errorf("GOODSENTRY_DEBUGINFOD_URLS permits at most four URLs")
	}
	for _, source := range c.DebugInfodURLs {
		endpoint, endpointErr := url.Parse(source)
		if len(source) > 2048 || endpointErr != nil || endpoint.Opaque != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(source, "#") {
			return c, fmt.Errorf("GOODSENTRY_DEBUGINFOD_URLS must contain absolute HTTP(S) URLs without credentials, query or fragment")
		}
	}
	archiveFlag := firstSet("GOODSENTRY_ENABLE_ARCHIVE", "GOODSENTRY_ENABLE_DUCKDB", "GLITCHTIP_ENABLE_DUCKDB")
	c.ArchiveEnabled, err = strconv.ParseBool(archiveFlag)
	if err != nil {
		return c, fmt.Errorf("GOODSENTRY_ENABLE_ARCHIVE must be true or false")
	}
	c.ArchiveBackend = strings.ToLower(strings.TrimSpace(value("GOODSENTRY_ARCHIVE_BACKEND", "local")))
	if c.ArchiveBackend != "local" && c.ArchiveBackend != "s3" {
		return c, fmt.Errorf("GOODSENTRY_ARCHIVE_BACKEND must be local or s3")
	}
	c.ArchiveDir = value("GOODSENTRY_ARCHIVE_DIR", filepath.Join(c.UploadDir, "archive"))
	c.ArchiveS3Bucket = strings.TrimSpace(os.Getenv("GOODSENTRY_ARCHIVE_S3_BUCKET"))
	c.ArchiveS3Region = strings.TrimSpace(value("GOODSENTRY_ARCHIVE_S3_REGION", value("AWS_REGION", "")))
	c.ArchiveS3Endpoint = strings.TrimSpace(os.Getenv("GOODSENTRY_ARCHIVE_S3_ENDPOINT"))
	c.ArchiveS3Prefix = strings.Trim(strings.TrimSpace(os.Getenv("GOODSENTRY_ARCHIVE_S3_PREFIX")), "/")
	if c.ArchiveS3Endpoint != "" {
		endpoint, endpointErr := url.Parse(c.ArchiveS3Endpoint)
		if endpointErr != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return c, fmt.Errorf("GOODSENTRY_ARCHIVE_S3_ENDPOINT must be an absolute http or https URL without credentials, query or fragment")
		}
	}
	c.DefaultEmail = strings.ToLower(strings.TrimSpace(value("GOODSENTRY_DEFAULT_EMAIL", "owner@localhost")))
	address, addressErr := mail.ParseAddress(c.DefaultEmail)
	if addressErr != nil || address.Address != c.DefaultEmail || len(c.DefaultEmail) > 254 {
		return c, fmt.Errorf("GOODSENTRY_DEFAULT_EMAIL must be a valid email address")
	}
	c.DefaultName = strings.TrimSpace(value("GOODSENTRY_DEFAULT_NAME", "GoodSentry Owner"))
	if c.DefaultName == "" || len(c.DefaultName) > 120 || !utf8.ValidString(c.DefaultName) || strings.ContainsFunc(c.DefaultName, unicode.IsControl) {
		return c, fmt.Errorf("GOODSENTRY_DEFAULT_NAME must contain 1 to 120 characters without controls")
	}
	c.DefaultOrganization = strings.TrimSpace(value("GOODSENTRY_DEFAULT_ORGANIZATION", "Default Organization"))
	if c.DefaultOrganization == "" || len(c.DefaultOrganization) > 120 || !utf8.ValidString(c.DefaultOrganization) || strings.ContainsFunc(c.DefaultOrganization, unicode.IsControl) {
		return c, fmt.Errorf("GOODSENTRY_DEFAULT_ORGANIZATION must contain 1 to 120 characters without controls")
	}
	c.DefaultPassword = os.Getenv("GOODSENTRY_DEFAULT_PASSWORD")
	if c.DefaultPassword != "" && (len(c.DefaultPassword) < 12 || len(c.DefaultPassword) > 72) {
		return c, fmt.Errorf("GOODSENTRY_DEFAULT_PASSWORD must contain 12 to 72 bytes")
	}
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
		{"GOODSENTRY_DEBUGINFOD_ALLOW_PRIVATE_IPS", "", false, &c.DebugInfodAllowPrivate},
		{"GOODSENTRY_ENABLE_MCP", "GLITCHTIP_ENABLE_MCP", false, &c.MCP},
		{"GOODSENTRY_ENABLE_ORGANIZATION_CREATION", "ENABLE_ORGANIZATION_CREATION", false, &c.OrganizationCreation},
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
		{"ISSUE_MAINTENANCE_INTERVAL_SECONDS", 3600, &c.IssueMaintenanceIntervalSeconds},
		{"GOODSENTRY_RETENTION_INTERVAL_SECONDS", 3600, &c.RetentionIntervalSeconds},
	} {
		if raw := os.Getenv(option.name); raw != "" {
			*option.dest, err = strconv.Atoi(raw)
			if err != nil || *option.dest < 1 || *option.dest > option.max {
				return c, fmt.Errorf("%s must be between 1 and %d", option.name, option.max)
			}
		}
	}
	for _, option := range []struct {
		name     string
		min, max int
		dest     *int
	}{
		{"GOODSENTRY_OTLP_MAX_RECORDS", 1, 100000, &c.OTLPMaxRecords},
		{"GOODSENTRY_OTLP_MAX_NODES", 100, 1000000, &c.OTLPMaxNodes},
	} {
		if raw := os.Getenv(option.name); raw != "" {
			*option.dest, err = strconv.Atoi(raw)
			if err != nil || *option.dest < option.min || *option.dest > option.max {
				return c, fmt.Errorf("%s must be between %d and %d", option.name, option.min, option.max)
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
	c.OIDCIssuer = strings.TrimRight(strings.TrimSpace(os.Getenv("GOODSENTRY_OIDC_ISSUER")), "/")
	c.OIDCClientID = strings.TrimSpace(os.Getenv("GOODSENTRY_OIDC_CLIENT_ID"))
	c.OIDCClientSecret = strings.TrimSpace(os.Getenv("GOODSENTRY_OIDC_CLIENT_SECRET"))
	c.OIDCName = strings.TrimSpace(value("GOODSENTRY_OIDC_NAME", "Single sign-on"))
	if c.OIDCIssuer != "" || c.OIDCClientID != "" || c.OIDCClientSecret != "" {
		issuer, err := url.Parse(c.OIDCIssuer)
		if err != nil || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" ||
			(issuer.Scheme != "https" && !(issuer.Scheme == "http" && (issuer.Hostname() == "localhost" || issuer.Hostname() == "127.0.0.1" || issuer.Hostname() == "::1"))) {
			return c, fmt.Errorf("GOODSENTRY_OIDC_ISSUER must be an absolute HTTPS URL, or HTTP on loopback")
		}
		if c.OIDCClientID == "" || len(c.OIDCClientID) > 256 || c.OIDCClientSecret == "" || len(c.OIDCClientSecret) > 1024 {
			return c, fmt.Errorf("GOODSENTRY_OIDC_CLIENT_ID and GOODSENTRY_OIDC_CLIENT_SECRET are required with an issuer")
		}
		if c.OIDCName == "" || len(c.OIDCName) > 80 || strings.ContainsFunc(c.OIDCName, unicode.IsControl) {
			return c, fmt.Errorf("GOODSENTRY_OIDC_NAME must be 1 to 80 characters without controls")
		}
		for _, scope := range strings.Fields(value("GOODSENTRY_OIDC_SCOPES", "openid profile email")) {
			if len(scope) > 64 || strings.ContainsFunc(scope, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r))
			}) {
				return c, fmt.Errorf("GOODSENTRY_OIDC_SCOPES contains an invalid scope")
			}
			c.OIDCScopes = append(c.OIDCScopes, scope)
		}
		foundOpenID := false
		for _, scope := range c.OIDCScopes {
			foundOpenID = foundOpenID || scope == "openid"
		}
		if !foundOpenID {
			return c, fmt.Errorf("GOODSENTRY_OIDC_SCOPES must include openid")
		}
		c.OIDCAutoCreate, err = strconv.ParseBool(value("GOODSENTRY_OIDC_AUTO_CREATE", "false"))
		if err != nil {
			return c, fmt.Errorf("GOODSENTRY_OIDC_AUTO_CREATE must be true or false")
		}
	}
	master := alias("GOODSENTRY_RETENTION_DAYS", "GLITCHTIP_RETENTION_DAYS", value("RETENTION_DAYS", "90"))
	c.RetentionDays, err = strconv.Atoi(master)
	if err != nil || c.RetentionDays < 1 || c.RetentionDays > 3650 {
		return c, fmt.Errorf("GOODSENTRY_RETENTION_DAYS / GLITCHTIP_RETENTION_DAYS / RETENTION_DAYS must be between 1 and 3650")
	}
	logs := alias("GOODSENTRY_LOG_RETENTION_DAYS", "GLITCHTIP_LOG_RETENTION_DAYS", strconv.Itoa(c.RetentionDays))
	c.LogRetentionDays, err = strconv.Atoi(logs)
	if err != nil || c.LogRetentionDays < 1 || c.LogRetentionDays > 3650 {
		return c, fmt.Errorf("GOODSENTRY_LOG_RETENTION_DAYS / GLITCHTIP_LOG_RETENTION_DAYS must be between 1 and 3650")
	}
	transactions := alias("GOODSENTRY_TRANSACTION_RETENTION_DAYS", "GLITCHTIP_TRANSACTION_RETENTION_DAYS", strconv.Itoa(c.RetentionDays))
	c.TransactionRetentionDays, err = strconv.Atoi(transactions)
	if err != nil || c.TransactionRetentionDays < 1 || c.TransactionRetentionDays > 3650 {
		return c, fmt.Errorf("GOODSENTRY_TRANSACTION_RETENTION_DAYS / GLITCHTIP_TRANSACTION_RETENTION_DAYS must be between 1 and 3650")
	}
	archiveAfterDefault := c.TransactionRetentionDays / 2
	if archiveAfterDefault < 1 {
		archiveAfterDefault = 1
	}
	if archiveAfterDefault > 30 {
		archiveAfterDefault = 30
	}
	c.ArchiveAfterDays = archiveAfterDefault
	if raw := strings.TrimSpace(os.Getenv("GOODSENTRY_ARCHIVE_AFTER_DAYS")); raw != "" {
		c.ArchiveAfterDays, err = strconv.Atoi(raw)
		if err != nil || c.ArchiveAfterDays < 1 || c.ArchiveAfterDays > c.TransactionRetentionDays {
			return c, fmt.Errorf("GOODSENTRY_ARCHIVE_AFTER_DAYS must be between 1 and GOODSENTRY_TRANSACTION_RETENTION_DAYS")
		}
	}
	if c.ArchiveEnabled && c.ArchiveBackend == "s3" {
		if c.ArchiveS3Bucket == "" || c.ArchiveS3Region == "" {
			return c, fmt.Errorf("GOODSENTRY_ARCHIVE_S3_BUCKET and GOODSENTRY_ARCHIVE_S3_REGION are required for the s3 archive backend")
		}
	}
	events := alias("GOODSENTRY_EVENT_RETENTION_DAYS", "GLITCHTIP_EVENT_RETENTION_DAYS", strconv.Itoa(c.RetentionDays))
	c.EventRetentionDays, err = strconv.Atoi(events)
	if err != nil || c.EventRetentionDays < 1 || c.EventRetentionDays > 3650 {
		return c, fmt.Errorf("GOODSENTRY_EVENT_RETENTION_DAYS / GLITCHTIP_EVENT_RETENTION_DAYS must be between 1 and 3650")
	}
	uptimeDays := alias("GOODSENTRY_UPTIME_RETENTION_DAYS", "GLITCHTIP_UPTIME_RETENTION_DAYS", strconv.Itoa(c.RetentionDays))
	c.UptimeRetentionDays, err = strconv.Atoi(uptimeDays)
	if err != nil || c.UptimeRetentionDays < 1 || c.UptimeRetentionDays > 3650 {
		return c, fmt.Errorf("GOODSENTRY_UPTIME_RETENTION_DAYS / GLITCHTIP_UPTIME_RETENTION_DAYS must be between 1 and 3650")
	}
	fileDays := alias("GOODSENTRY_FILE_RETENTION_DAYS", "GLITCHTIP_FILE_RETENTION_DAYS", strconv.Itoa(c.RetentionDays))
	c.FileRetentionDays, err = strconv.Atoi(fileDays)
	if err != nil || c.FileRetentionDays < 1 || c.FileRetentionDays > 3650 {
		return c, fmt.Errorf("GOODSENTRY_FILE_RETENTION_DAYS / GLITCHTIP_FILE_RETENTION_DAYS must be between 1 and 3650")
	}
	releaseDays := alias("GOODSENTRY_RELEASE_RETENTION_DAYS", "GLITCHTIP_RELEASE_RETENTION_DAYS", "365")
	c.ReleaseRetentionDays, err = strconv.Atoi(releaseDays)
	if err != nil || c.ReleaseRetentionDays < 1 || c.ReleaseRetentionDays > 3650 {
		return c, fmt.Errorf("GOODSENTRY_RELEASE_RETENTION_DAYS / GLITCHTIP_RELEASE_RETENTION_DAYS must be between 1 and 3650")
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
	c.Secret, err = loadSecret(c.Secret, c.AuthMode, c.UploadDir)
	if err != nil {
		return c, err
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

func firstSet(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return "false"
}
