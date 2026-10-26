package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"goodsentry/internal/store"
)

type apiGrantKey struct{}

func (s *Server) compatible(scope string, next http.HandlerFunc) http.Handler {
	return s.compatibleAny([]string{scope}, next)
}

func (s *Server) compatibleAny(scopes []string, next http.HandlerFunc) http.Handler {
	session := s.auth(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) == 0 {
			session.ServeHTTP(w, r)
			return
		}
		parts := strings.Fields(values[0])
		if len(values) != 1 || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			failure(w, 401, "A valid Bearer credential is required")
			return
		}
		var grant store.APITokenGrant
		var err error
		switch {
		case strings.HasPrefix(parts[1], "gs_"):
			if !validAPICredential(parts[1]) {
				failure(w, 401, "A valid Bearer credential is required")
				return
			}
			grant, err = s.db.AuthenticateAPIToken(r.Context(), s.hash(parts[1]))
		case validUserAPICredential(parts[1]):
			grant, err = s.db.AuthenticateUserAPIToken(r.Context(), s.hash(parts[1]))
		default:
			failure(w, 401, "A valid Bearer credential is required")
			return
		}
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				failure(w, 401, "Credential is invalid, expired or revoked")
			} else {
				report(w, err)
			}
			return
		}
		if !s.allowTwoFactorGate(w, r, grant.User.ID) {
			return
		}
		if len(scopes) > 0 && !slices.ContainsFunc(scopes, func(scope string) bool { return slices.Contains(grant.Scopes, scope) }) {
			failure(w, 403, "Credential requires one of these scopes: "+strings.Join(scopes, ", "))
			return
		}
		ctx := context.WithValue(r.Context(), identityKey{}, identity{User: grant.User})
		ctx = context.WithValue(ctx, apiGrantKey{}, grant)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validAPICredential(value string) bool {
	if len(value) != 67 || !strings.HasPrefix(value, "gs_") {
		return false
	}
	for _, c := range value[3:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validUserAPICredential recognizes a compatible account token: exactly 32
// secret bytes in lowercase hexadecimal, distinct from native gs_ credentials.
func validUserAPICredential(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *Server) managementAccess(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	userID := current(r).User.ID
	org, err := s.db.ManagementOrganization(r.Context(), userID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return store.ManagementAccess{}, false
	}
	access := store.ManagementAccess{UserID: userID, OrganizationID: org.ID, Role: org.Role, AllProjects: true}
	if grant, ok := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); ok {
		if grant.OrganizationID != "" {
			if grant.OrganizationID != org.ID {
				failure(w, 404, "Not found")
				return store.ManagementAccess{}, false
			}
			access.AllProjects, access.ProjectIDs = grant.AllProjects, grant.ProjectIDs
			access.ReadOnly = !slices.Contains(grant.Scopes, "project:releases")
		} else {
			// Account tokens carry no bound organization; the membership lookup
			// above already answered 404 outside the owner's workspaces.
			access.ReadOnly = !slices.ContainsFunc(grant.Scopes, writeScope)
		}
	}
	return access, true
}

func writeScope(scope string) bool {
	switch scope {
	case "project:write", "project:admin", "project:releases", "event:write", "event:admin", "org:write", "org:admin", "team:write", "team:admin", "member:write", "member:admin":
		return true
	}
	return false
}
