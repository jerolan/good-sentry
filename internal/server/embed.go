package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"goodsentry/internal/store"
)

const (
	embedDefaultTitle     = "It looks like we're having issues."
	embedDefaultSubtitle  = "Our team has been notified."
	embedDefaultSubtitle2 = "If you'd like to help, tell us what happened below."
	embedDefaultName      = "Name"
	embedDefaultEmail     = "Email"
	embedDefaultComments  = "What happened?"
	embedDefaultClose     = "Close"
	embedDefaultSubmit    = "Submit Crash Report"
	embedGenericError     = "An unknown error occurred while submitting your report. Please try again."
	embedFormError        = "Some fields were invalid. Please correct the errors and try again."
	embedSuccess          = "Your feedback has been sent. Thank you!"
)

type embedConfig struct {
	Title         string `json:"title"`
	Subtitle      string `json:"subtitle"`
	Subtitle2     string `json:"subtitle2"`
	LabelName     string `json:"labelName"`
	LabelEmail    string `json:"labelEmail"`
	LabelComments string `json:"labelComments"`
	LabelClose    string `json:"labelClose"`
	LabelSubmit   string `json:"labelSubmit"`
	ErrorGeneric  string `json:"errorGeneric"`
	ErrorForm     string `json:"errorForm"`
	Success       string `json:"success"`
	Name          string `json:"name"`
	Email         string `json:"email"`
}

func embedQuery(query url.Values, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(query.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func embedDSN(raw string) (key, projectID string, ok bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User == nil {
		return "", "", false
	}
	key = parsed.User.Username()
	projectID = path.Base(strings.TrimSuffix(parsed.Path, "/"))
	if key == "" || projectID == "." || projectID == "/" || projectID == "" {
		return "", "", false
	}
	if _, err := strconv.ParseInt(projectID, 10, 64); err != nil {
		return "", "", false
	}
	return key, projectID, true
}

func embedPageConfig(query url.Values) embedConfig {
	config := embedConfig{
		Title: embedDefaultTitle, Subtitle: embedDefaultSubtitle, Subtitle2: embedDefaultSubtitle2,
		LabelName: embedDefaultName, LabelEmail: embedDefaultEmail, LabelComments: embedDefaultComments,
		LabelClose: embedDefaultClose, LabelSubmit: embedDefaultSubmit,
		ErrorGeneric: embedGenericError, ErrorForm: embedFormError, Success: embedSuccess,
	}
	for _, item := range []struct {
		field string
		names []string
	}{
		{"title", []string{"title"}}, {"subtitle", []string{"subtitle"}}, {"subtitle2", []string{"subtitle2"}},
		{"labelName", []string{"labelName", "label_name"}}, {"labelEmail", []string{"labelEmail", "label_email"}},
		{"labelComments", []string{"labelComments", "label_comments"}}, {"labelClose", []string{"labelClose", "label_close"}},
		{"labelSubmit", []string{"labelSubmit", "label_submit"}}, {"errorGeneric", []string{"errorGeneric", "error_generic"}},
		{"errorForm", []string{"errorFormEntry", "error_form_entry"}}, {"success", []string{"successMessage", "success_message"}},
		{"name", []string{"name"}}, {"email", []string{"email"}},
	} {
		if value := embedQuery(query, item.names...); value != "" {
			switch item.field {
			case "title":
				config.Title = value
			case "subtitle":
				config.Subtitle = value
			case "subtitle2":
				config.Subtitle2 = value
			case "labelName":
				config.LabelName = value
			case "labelEmail":
				config.LabelEmail = value
			case "labelComments":
				config.LabelComments = value
			case "labelClose":
				config.LabelClose = value
			case "labelSubmit":
				config.LabelSubmit = value
			case "errorGeneric":
				config.ErrorGeneric = value
			case "errorForm":
				config.ErrorForm = value
			case "success":
				config.Success = value
			case "name":
				config.Name = value
			case "email":
				config.Email = value
			}
		}
	}
	return config
}

func (s *Server) embedErrorPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	key, projectID, ok := embedDSN(query.Get("dsn"))
	if !ok {
		failure(w, 401, "Invalid DSN")
		return
	}
	eventID := embedQuery(query, "eventId", "event_id")
	if eventID == "" {
		failure(w, 400, "eventId is required")
		return
	}
	valid, err := s.db.ValidEmbedKey(r.Context(), key, projectID)
	if err != nil {
		report(w, err)
		return
	}
	if !valid {
		failure(w, 401, "Invalid DSN")
		return
	}
	if r.Method == http.MethodGet {
		config, err := json.Marshal(embedPageConfig(query))
		if err != nil {
			report(w, err)
			return
		}
		script := strings.Replace(embedScript, "__CONFIG__", string(config), 1)
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(script))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		failure(w, 400, "Invalid form body")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	email := strings.TrimSpace(r.PostForm.Get("email"))
	comments := strings.TrimSpace(r.PostForm.Get("comments"))
	formErrors := map[string]string{}
	if len([]rune(name)) > 128 {
		formErrors["name"] = "Name is too long"
	}
	if len([]rune(email)) > 254 {
		formErrors["email"] = "Email is too long"
	}
	if comments == "" {
		formErrors["comments"] = "This field is required."
	}
	if len([]rune(comments)) > 10000 {
		formErrors["comments"] = "Comments are too long"
	}
	if len(formErrors) > 0 {
		writeJSON(w, 400, map[string]any{"errors": formErrors})
		return
	}
	_, err = s.db.SubmitEmbeddedUserReport(r.Context(), key, projectID, store.UserReportInput{
		EventID: eventID, Name: name, Email: email, Comments: comments,
	})
	if errors.Is(err, store.ErrInvalidUserReport) {
		writeJSON(w, 400, map[string]any{"errors": map[string]string{"comments": "Invalid report"}})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		failure(w, 401, "Invalid DSN")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

const embedScript = `(function () {
  'use strict';
  var config = __CONFIG__;
  if (document.querySelector('.sentry-error-embed-wrapper')) return;
  var script = document.currentScript;
  var endpoint = script ? script.src : window.location.href;
  var css = '.sentry-error-embed-wrapper{position:fixed;inset:0;z-index:9999;background:rgba(20,25,35,.65);display:flex;align-items:flex-start;justify-content:center;padding:24px;overflow:auto}.sentry-error-embed{box-sizing:border-box;width:100%;max-width:700px;margin-top:4vh;padding:32px;border-radius:12px;background:#fff;color:#546076;box-shadow:0 20px 60px rgba(0,0,0,.35);font-family:system-ui,-apple-system,sans-serif}.sentry-error-embed h2{margin:0 0 6px;font-size:26px;color:#394359}.sentry-error-embed header{text-align:center;border-bottom:1px solid #e8ebf1;padding-bottom:16px;margin-bottom:20px}.sentry-error-embed header p{margin:0;color:#94a0b3;font-size:15px}.sentry-error-embed label{display:block;font-size:11px;font-weight:600;text-transform:uppercase;margin:0 0 6px}.sentry-error-embed .field{margin-bottom:16px}.sentry-error-embed input,.sentry-error-embed textarea{box-sizing:border-box;width:100%;padding:9px 11px;border:1px solid #bac2d3;border-radius:6px;background:#fbfbfc;font-size:15px;color:#394359}.sentry-error-embed textarea{min-height:100px;resize:vertical}.sentry-error-embed .actions{display:flex;align-items:center;gap:16px;flex-wrap:wrap}.sentry-error-embed button{border:0;border-radius:6px;padding:10px 15px;font-size:15px;cursor:pointer}.sentry-error-embed .submit{background:#25a6f7;color:#fff}.sentry-error-embed .submit:disabled{opacity:.6;cursor:wait}.sentry-error-embed .close{background:transparent;color:#546076;padding-left:0}.sentry-error-embed .status{min-height:20px;margin:0 0 12px;font-size:14px;color:#c0392b}.sentry-error-embed .success{margin:0;padding:24px;text-align:center;border:1px solid #c9dec2;background:#fcfefb;color:#394359;border-radius:8px}@media(max-width:520px){.sentry-error-embed-wrapper{padding:0}.sentry-error-embed{margin:0;min-height:100%;border-radius:0;padding:20px}.sentry-error-embed h2{font-size:21px}}';
  var style = document.createElement('style');
  style.textContent = css;
  document.head.appendChild(style);
  var wrapper = document.createElement('div');
  wrapper.className = 'sentry-error-embed-wrapper';
  wrapper.setAttribute('role', 'dialog');
  wrapper.setAttribute('aria-modal', 'true');
  var box = document.createElement('div');
  box.className = 'sentry-error-embed';
  var header = document.createElement('header');
  var heading = document.createElement('h2'); heading.textContent = config.title;
  var subtitle = document.createElement('p'); subtitle.textContent = config.subtitle + ' ' + config.subtitle2;
  header.appendChild(heading); header.appendChild(subtitle); box.appendChild(header);
  var form = document.createElement('form');
  var status = document.createElement('p'); status.className = 'status'; status.setAttribute('role', 'alert');
  form.appendChild(status);
  function field(name, label, value, textarea) {
    var wrap = document.createElement('div'); wrap.className = 'field';
    var text = document.createElement('label'); text.textContent = label; text.htmlFor = 'sentry-embed-' + name;
    var input = document.createElement(textarea ? 'textarea' : 'input');
    input.id = 'sentry-embed-' + name; input.name = name; input.value = value || '';
    if (!textarea) input.type = 'text';
    wrap.appendChild(text); wrap.appendChild(input); form.appendChild(wrap);
    return input;
  }
  var name = field('name', config.labelName, config.name, false);
  var email = field('email', config.labelEmail, config.email, false);
  var comments = field('comments', config.labelComments, '', true);
  var actions = document.createElement('div'); actions.className = 'actions';
  var submit = document.createElement('button'); submit.type = 'submit'; submit.className = 'submit'; submit.textContent = config.labelSubmit;
  var close = document.createElement('button'); close.type = 'button'; close.className = 'close'; close.textContent = config.labelClose;
  actions.appendChild(submit); actions.appendChild(close); form.appendChild(actions);
  function dismiss() { if (wrapper.parentNode) wrapper.parentNode.removeChild(wrapper); }
  close.addEventListener('click', dismiss);
  wrapper.addEventListener('click', function (event) { if (event.target === wrapper) dismiss(); });
  form.addEventListener('submit', function (event) {
    event.preventDefault();
    if (submit.disabled) return;
    submit.disabled = true; status.textContent = '';
    var body = new URLSearchParams();
    body.set('name', name.value); body.set('email', email.value); body.set('comments', comments.value);
    fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: body.toString(), credentials: 'omit' })
      .then(function (response) {
        if (response.ok) {
          form.textContent = '';
          var success = document.createElement('p'); success.className = 'success'; success.textContent = config.success;
          form.appendChild(success);
        } else if (response.status === 400) {
          status.textContent = config.errorForm;
        } else {
          status.textContent = config.errorGeneric;
        }
      })
      .catch(function () { status.textContent = config.errorGeneric; })
      .then(function () { submit.disabled = false; });
  });
  box.appendChild(form); wrapper.appendChild(box);
  function mount() { (document.body || document.documentElement).appendChild(wrapper); }
  if (document.body) mount(); else document.addEventListener('DOMContentLoaded', mount, { once: true });
}());`
