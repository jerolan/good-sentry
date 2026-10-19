package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

//go:embed web/index.html web/app.css web/*.js
var web embed.FS

type Server struct {
	cfg config.Config
	db  *store.Store
}
type identity struct {
	User store.User
	CSRF string
}
type identityKey struct{}

func New(cfg config.Config, db *store.Store) http.Handler {
	s := &Server{cfg: cfg, db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "GoodSentry"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ready(ctx); err != nil {
			failure(w, 503, "Database unavailable")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/setup", s.setupState)
	mux.HandleFunc("POST /api/v1/setup", s.setup)
	mux.HandleFunc("POST /api/v1/login", s.login)
	mux.Handle("GET /api/v1/me", s.auth(http.HandlerFunc(s.me)))
	mux.Handle("POST /api/v1/logout", s.auth(http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/v1/organizations/{org}/api-tokens", s.auth(http.HandlerFunc(s.apiTokens)))
	mux.Handle("POST /api/v1/organizations/{org}/api-tokens", s.auth(http.HandlerFunc(s.createAPIToken)))
	mux.Handle("DELETE /api/v1/organizations/{org}/api-tokens/{token}", s.auth(http.HandlerFunc(s.revokeAPIToken)))
	mux.Handle("GET /api/0/organizations/{org}/projects/{$}", s.compatible("org:read", s.compatibleProjects))
	mux.Handle("GET /api/0/projects/{org}/{project}/{$}", s.compatible("project:read", s.compatibleProject))
	s.registerCompatibleEventRoutes(mux)
	s.registerCompatibleAttachmentRoutes(mux)
	s.registerCompatibleIssueRoutes(mux)
	s.registerUserReportRoutes(mux)
	s.registerFeedbackRoutes(mux)
	s.registerCompatibleIssueWriteRoutes(mux)
	s.registerCompatibleIssueDeleteRoutes(mux)
	s.registerReleaseRoutes(mux)
	s.registerArtifactRoutes(mux)
	mux.Handle("GET /api/v1/organizations/{org}/projects", s.auth(http.HandlerFunc(s.projects)))
	mux.Handle("POST /api/v1/organizations/{org}/projects", s.auth(http.HandlerFunc(s.createProject)))
	mux.Handle("GET /api/v1/organizations/{org}/projects/{project}/sourcemaps", s.auth(http.HandlerFunc(s.sourceMaps)))
	mux.Handle("POST /api/v1/organizations/{org}/projects/{project}/sourcemaps", s.auth(http.HandlerFunc(s.uploadSourceMap)))
	mux.Handle("GET /api/v1/organizations/{org}/projects/{project}/alerts", s.auth(http.HandlerFunc(s.alerts)))
	mux.Handle("POST /api/v1/organizations/{org}/projects/{project}/alerts", s.auth(http.HandlerFunc(s.saveAlert)))
	mux.Handle("GET /api/v1/organizations/{org}/projects/{project}/alerts/{rule}", s.auth(http.HandlerFunc(s.alert)))
	mux.Handle("PUT /api/v1/organizations/{org}/projects/{project}/alerts/{rule}", s.auth(http.HandlerFunc(s.saveAlert)))
	mux.Handle("DELETE /api/v1/organizations/{org}/projects/{project}/alerts/{rule}", s.auth(http.HandlerFunc(s.deleteAlert)))
	mux.Handle("POST /api/v1/organizations/{org}/projects/{project}/alerts/{rule}/test", s.auth(http.HandlerFunc(s.testAlert)))
	mux.Handle("GET /api/v1/organizations/{org}/projects/{project}/alert-deliveries", s.auth(http.HandlerFunc(s.alertDeliveries)))
	mux.Handle("GET /api/v1/organizations/{org}/projects/{project}/alert-deliveries/{delivery}", s.auth(http.HandlerFunc(s.alertDelivery)))
	mux.Handle("GET /api/v1/organizations/{org}/issues", s.auth(http.HandlerFunc(s.issues)))
	mux.Handle("POST /api/v1/organizations/{org}/issues/merge", s.auth(http.HandlerFunc(s.mergeIssues)))
	mux.Handle("GET /api/v1/issues/{issue}", s.auth(http.HandlerFunc(s.issue)))
	mux.Handle("GET /api/v1/issues/{issue}/events/{event}/attachments", s.auth(http.HandlerFunc(s.issueEventAttachments)))
	mux.Handle("GET /api/v1/issues/{issue}/events/{event}/attachments/{attachment}", s.auth(http.HandlerFunc(s.issueEventAttachment)))
	mux.Handle("PATCH /api/v1/issues/{issue}", s.auth(http.HandlerFunc(s.triage)))
	mux.Handle("POST /api/v1/issues/{issue}/unmerge", s.auth(http.HandlerFunc(s.unmergeIssue)))
	mux.Handle("GET /api/v1/issues/{issue}/origins", s.auth(http.HandlerFunc(s.issueOrigins)))
	mux.Handle("GET /api/v1/issues/{issue}/assignees", s.auth(http.HandlerFunc(s.issueAssignees)))
	mux.Handle("GET /api/v1/issues/{issue}/releases", s.auth(http.HandlerFunc(s.issueReleases)))
	mux.Handle("GET /api/v1/issues/{issue}/activity", s.auth(http.HandlerFunc(s.issueActivity)))
	mux.Handle("GET /api/v1/issues/{issue}/comments", s.auth(http.HandlerFunc(s.issueComments)))
	mux.Handle("POST /api/v1/issues/{issue}/comments", s.auth(http.HandlerFunc(s.addComment)))
	mux.Handle("PATCH /api/v1/issues/{issue}/comments/{comment}", s.auth(http.HandlerFunc(s.editComment)))
	mux.Handle("DELETE /api/v1/issues/{issue}/comments/{comment}", s.auth(http.HandlerFunc(s.deleteComment)))
	mux.Handle("GET /api/v1/organizations/{org}/transactions", s.auth(http.HandlerFunc(s.transactions)))
	mux.Handle("GET /api/v1/organizations/{org}/transactions/{source}/{id}", s.auth(http.HandlerFunc(s.transactionDetail)))
	mux.Handle("GET /api/v1/organizations/{org}/transactions/{source}/{id}/span-groups", s.auth(http.HandlerFunc(s.transactionSpanGroups)))
	mux.Handle("GET /api/v1/organizations/{org}/traces/{trace}", s.auth(http.HandlerFunc(s.trace)))
	mux.Handle("GET /api/v1/organizations/{org}/traces/{trace}/nodes/{node}", s.auth(http.HandlerFunc(s.traceNode)))
	mux.Handle("GET /api/v1/organizations/{org}/traces/{trace}/errors", s.auth(http.HandlerFunc(s.traceErrors)))
	mux.Handle("GET /api/v1/organizations/{org}/spans", s.auth(http.HandlerFunc(s.spans)))
	mux.Handle("GET /api/v1/organizations/{org}/logs", s.auth(http.HandlerFunc(s.logs)))
	mux.Handle("GET /api/v1/organizations/{org}/logs/{log}", s.auth(http.HandlerFunc(s.logDetail)))
	if cfg.Uptime {
		mux.Handle("GET /api/v1/organizations/{org}/monitors", s.auth(http.HandlerFunc(s.monitors)))
		mux.Handle("POST /api/v1/organizations/{org}/monitors", s.auth(http.HandlerFunc(s.saveMonitor)))
		mux.Handle("GET /api/v1/organizations/{org}/monitors/{monitor}", s.auth(http.HandlerFunc(s.monitor)))
		mux.Handle("PUT /api/v1/organizations/{org}/monitors/{monitor}", s.auth(http.HandlerFunc(s.saveMonitor)))
		mux.Handle("DELETE /api/v1/organizations/{org}/monitors/{monitor}", s.auth(http.HandlerFunc(s.deleteMonitor)))
		mux.Handle("GET /api/v1/organizations/{org}/monitors/{monitor}/checks", s.auth(http.HandlerFunc(s.monitorChecks)))
		mux.HandleFunc("POST /api/v1/heartbeat/{key}", s.heartbeat)
		mux.HandleFunc("/api/v1/heartbeat/{key}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", "POST")
			failure(w, 405, "Heartbeat requires POST")
		})
	}
	mux.HandleFunc("POST /api/{project}/envelope/", s.ingest)
	mux.HandleFunc("POST /api/{project}/store/", s.ingest)
	mux.HandleFunc("OPTIONS /api/{project}/envelope/", s.preflight)
	mux.HandleFunc("OPTIONS /api/{project}/store/", s.preflight)
	s.registerSecurityReportRoutes(mux)
	mux.HandleFunc("/", s.assets)
	var handler http.Handler = mux
	if cfg.Prefix != "" {
		stripped := http.StripPrefix(cfg.Prefix, mux)
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == cfg.Prefix {
				http.Redirect(w, r, cfg.Prefix+"/", http.StatusPermanentRedirect)
				return
			}
			if !strings.HasPrefix(r.URL.Path, cfg.Prefix+"/") {
				http.NotFound(w, r)
				return
			}
			stripped.ServeHTTP(w, r)
		})
	}
	handler = s.otlpRoutes(handler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'self'; frame-ancestors 'none'")
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("HTTP handler panicked", "path", r.URL.Path, "panic", recovered)
				failure(w, 500, "Internal server error")
			}
		}()
		handler.ServeHTTP(w, r)
	})
}

func (s *Server) assets(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		failure(w, 405, "Method not allowed")
		return
	}
	if r.URL.Path == "/" {
		body, err := web.ReadFile("web/index.html")
		if err != nil {
			failure(w, 500, "Interface unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, strings.ReplaceAll(string(body), "__GOODSENTRY_BASE__", html.EscapeString(s.cfg.Prefix)))
		return
	}
	if r.URL.Path != "/app.css" && r.URL.Path != "/app.js" && r.URL.Path != "/alerts.js" && r.URL.Path != "/releases.js" && r.URL.Path != "/hashes.js" && r.URL.Path != "/reports.js" && r.URL.Path != "/feedback.js" {
		failure(w, 404, "Not found")
		return
	}
	sub, _ := fs.Sub(web, "web")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	if status != 204 {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func failure(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"detail": message})
}
func report(w http.ResponseWriter, err error) {
	var merged *store.IssueMergedError
	if errors.As(err, &merged) {
		writeJSON(w, 409, map[string]string{"detail": merged.Error(), "mergedInto": merged.MergedInto})
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		failure(w, 404, "Not found")
	case errors.Is(err, store.ErrConflict):
		failure(w, 409, "Resource already exists")
	case errors.Is(err, store.ErrForbidden):
		failure(w, 403, "Permission denied")
	default:
		slog.Error("Database operation failed", "error", err)
		failure(w, 503, "Storage is temporarily unavailable")
	}
}
func bodyJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		failure(w, 400, "Invalid JSON request")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		failure(w, 400, "Expected one JSON value")
		return false
	}
	return true
}
func current(r *http.Request) identity { return r.Context().Value(identityKey{}).(identity) }
