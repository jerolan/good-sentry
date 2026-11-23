package portal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed templates/portal.html
var templateFiles embed.FS

//go:embed templates/app.css
var stylesheet []byte

var views = template.Must(template.ParseFS(templateFiles, "templates/portal.html"))

type portal struct {
	db        *pgxpool.Pool
	publicURL url.URL
	operator  [32]byte
	password  [32]byte
}

// New serves the authenticated operator portal and public SDK ingestion endpoint.
func New(db *pgxpool.Pool, publicURL url.URL, user, password string) http.Handler {
	p := &portal{
		db:        db,
		publicURL: publicURL,
		operator:  sha256.Sum256([]byte(user)),
		password:  sha256.Sum256([]byte(password)),
	}
	operator := http.NewServeMux()
	operator.HandleFunc("GET /{$}", p.listProjects)
	operator.HandleFunc("POST /projects", p.addProject)
	operator.HandleFunc("GET /projects/{project}", p.showProject)
	operator.HandleFunc("POST /projects/{project}/webhook", p.updateWebhook)
	operator.HandleFunc("GET /projects/{project}/issues/{issue}", p.showIssue)
	operator.HandleFunc("GET /projects/{project}/issues/{issue}/events/{event}", p.showEvent)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", p.health)
	mux.HandleFunc("GET /app.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		http.ServeContent(w, r, "app.css", time.Time{}, bytes.NewReader(stylesheet))
	})
	mux.HandleFunc("POST /api/{project}/envelope/{$}", p.ingest)
	mux.Handle("/", http.NewCrossOriginProtection().Handler(p.authenticate(operator)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}

func (p *portal) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		user, password, provided := r.BasicAuth()
		userHash := sha256.Sum256([]byte(user))
		passwordHash := sha256.Sum256([]byte(password))
		valid := subtle.ConstantTimeCompare(userHash[:], p.operator[:]) &
			subtle.ConstantTimeCompare(passwordHash[:], p.password[:])
		if !provided || valid != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoodSentry", charset="UTF-8"`)
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *portal) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := p.db.Ping(ctx); err != nil {
		http.Error(w, "PostgreSQL unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *portal) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := p.projects(r.Context())
	if err != nil {
		readError(w, r, "list projects", err)
		return
	}
	render(w, "projects", struct{ Projects []project }{projects})
}

func (p *portal) addProject(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid project form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || strings.ContainsRune(name, 0) {
		http.Error(w, "Project name must contain 1 to 80 characters", http.StatusBadRequest)
		return
	}
	id, err := p.createProject(r.Context(), name)
	if err != nil {
		readError(w, r, "create project", err)
		return
	}
	http.Redirect(w, r, "/projects/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (p *portal) updateWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid webhook form", http.StatusBadRequest)
		return
	}
	value := strings.TrimSpace(r.PostForm.Get("url"))
	if !validWebhookURL(value) {
		http.Error(w, "Invalid webhook URL", http.StatusBadRequest)
		return
	}
	projectID := positiveID(r.PathValue("project"))
	if err := p.setWebhook(r.Context(), projectID, value); err != nil {
		readError(w, r, "update webhook", err)
		return
	}
	http.Redirect(w, r, "/projects/"+strconv.FormatInt(projectID, 10), http.StatusSeeOther)
}

func (p *portal) showProject(w http.ResponseWriter, r *http.Request) {
	projectID := positiveID(r.PathValue("project"))
	selectedProject, err := p.project(r.Context(), projectID)
	if err != nil {
		readError(w, r, "read project", err)
		return
	}
	issues, err := p.issues(r.Context(), projectID)
	if err != nil {
		readError(w, r, "list issues", err)
		return
	}
	dsn := p.publicURL
	dsn.User = url.User(selectedProject.Key)
	dsn.Path = "/" + strconv.FormatInt(selectedProject.ID, 10)
	render(w, "project", struct {
		Project project
		DSN     string
		Issues  []issue
	}{selectedProject, dsn.String(), issues})
}

func (p *portal) showIssue(w http.ResponseWriter, r *http.Request) {
	projectID, issueID := positiveID(r.PathValue("project")), positiveID(r.PathValue("issue"))
	selectedIssue, err := p.issue(r.Context(), projectID, issueID)
	if err != nil {
		readError(w, r, "read issue", err)
		return
	}
	selectedProject, err := p.project(r.Context(), projectID)
	if err != nil {
		readError(w, r, "read project", err)
		return
	}
	events, err := p.events(r.Context(), projectID, issueID)
	if err != nil {
		readError(w, r, "list occurrences", err)
		return
	}
	render(w, "issue", struct {
		Project project
		Issue   issue
		Events  []event
	}{selectedProject, selectedIssue, events})
}

func (p *portal) showEvent(w http.ResponseWriter, r *http.Request) {
	projectID, issueID := positiveID(r.PathValue("project")), positiveID(r.PathValue("issue"))
	id := r.PathValue("event")
	if !validEventID(id) {
		http.NotFound(w, r)
		return
	}
	selectedEvent, err := p.event(r.Context(), projectID, issueID, strings.ToLower(id))
	if err != nil {
		readError(w, r, "read occurrence", err)
		return
	}
	selectedIssue, err := p.issue(r.Context(), projectID, issueID)
	if err != nil {
		readError(w, r, "read issue", err)
		return
	}
	selectedProject, err := p.project(r.Context(), projectID)
	if err != nil {
		readError(w, r, "read project", err)
		return
	}
	render(w, "event", struct {
		Project project
		Issue   issue
		Event   event
	}{selectedProject, selectedIssue, selectedEvent})
}

func (p *portal) ingest(w http.ResponseWriter, r *http.Request) {
	projectID := positiveID(r.PathValue("project"))
	key := ingestKey(r)
	if projectID == 0 || key == "" {
		http.Error(w, "Invalid project key", http.StatusForbidden)
		return
	}
	selectedProject, err := p.project(r.Context(), projectID)
	if errors.Is(err, pgx.ErrNoRows) ||
		(err == nil && subtle.ConstantTimeCompare([]byte(key), []byte(selectedProject.Key)) != 1) {
		http.Error(w, "Invalid project key", http.StatusForbidden)
		return
	}
	if err != nil {
		readError(w, r, "authenticate envelope", err)
		return
	}
	event, err := readEnvelope(w, r)
	switch {
	case errors.Is(err, errContentEncoding):
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
	case errors.Is(err, errEnvelopeTooBig):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
	case event.ID == "":
		w.WriteHeader(http.StatusNoContent)
	default:
		issueID, err := p.saveEvent(r.Context(), projectID, event)
		if err != nil {
			readError(w, r, "save occurrence", err)
			return
		}
		if issueID != 0 && selectedProject.WebhookURL != "" {
			sendWebhook(selectedProject.WebhookURL, projectID, issueID, event.ID)
		}
		w.Header().Set("Content-Type", "application/json")
		// Validation restricts event.ID to hexadecimal characters.
		w.Write([]byte(`{"id":"` + event.ID + `"}`))
	}
}

func positiveID(raw string) int64 {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func render(w http.ResponseWriter, name string, data any) {
	var body bytes.Buffer
	if err := views.ExecuteTemplate(&body, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "Portal unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	body.WriteTo(w)
}

func readError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	log.Printf("%s failed", operation)
	http.Error(w, "PostgreSQL operation failed", http.StatusInternalServerError)
}
