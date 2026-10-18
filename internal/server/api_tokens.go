package server

import (
	"math"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"goodsentry/internal/store"
)

func credentialText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func credentialQuery(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Query encoding is invalid")
		return nil, false
	}
	return query, true
}

func (s *Server) apiTokens(w http.ResponseWriter, r *http.Request) {
	query, valid := credentialQuery(w, r)
	if !valid {
		return
	}
	page := int64(1)
	if query.Has("page") {
		var valid bool
		page, valid = occurrenceNumber(query.Get("page"), 1000000)
		if !valid {
			failure(w, 400, "Page must be a positive decimal integer up to 1000000")
			return
		}
	}
	result, err := s.db.APITokens(r.Context(), current(r).User.ID, r.PathValue("org"), int(page))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	scopes := []string{"org:read", "project:read"}
	projects := []string{}
	days := 90
	input := struct {
		Name          string    `json:"name"`
		Scopes        *[]string `json:"scopes"`
		ProjectIDs    *[]string `json:"projectIds"`
		ExpiresInDays *int      `json:"expiresInDays"`
	}{Scopes: &scopes, ProjectIDs: &projects, ExpiresInDays: &days}
	if !bodyJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" || !credentialText(input.Name, 100) {
		failure(w, 400, "Name must contain 1 to 100 characters without controls")
		return
	}
	if input.Scopes == nil || len(*input.Scopes) == 0 || input.ProjectIDs == nil || input.ExpiresInDays == nil || *input.ExpiresInDays < 1 || *input.ExpiresInDays > 366 {
		failure(w, 400, "Provide scopes, project IDs and a lifetime from 1 to 366 days; null is not allowed")
		return
	}
	seen := map[string]bool{}
	for _, scope := range *input.Scopes {
		if (scope != "org:read" && scope != "project:read" && scope != "project:releases" && scope != "event:read" && scope != "event:write" && scope != "event:admin" && scope != "project:write") || seen[scope] {
			failure(w, 400, "Scopes must be distinct implemented values: org:read, project:read, project:releases, project:write, event:read, event:write or event:admin")
			return
		}
		seen[scope] = true
	}
	ids := make([]int64, 0, len(*input.ProjectIDs))
	for _, project := range *input.ProjectIDs {
		id, valid := occurrenceNumber(project, math.MaxInt64)
		if !valid {
			failure(w, 400, "Project IDs must be positive decimal int64 strings")
			return
		}
		ids = append(ids, id)
	}
	secret, err := token()
	if err != nil {
		failure(w, 503, "Cannot create a credential; retry shortly")
		return
	}
	secret = "gs_" + secret
	result, err := s.db.CreateAPIToken(r.Context(), current(r).User.ID, r.PathValue("org"), store.APITokenInput{
		Name: input.Name, Scopes: *input.Scopes, ProjectIDs: ids, ExpiresInDays: *input.ExpiresInDays,
	}, s.hash(secret), secret[:11])
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, struct {
		store.APIToken
		Token string `json:"token"`
	}{APIToken: result, Token: secret})
}

func (s *Server) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if _, valid := occurrenceNumber(r.PathValue("token"), math.MaxInt64); !valid {
		failure(w, 400, "Token ID must be a positive decimal int64")
		return
	}
	if err := s.db.RevokeAPIToken(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("token")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}
