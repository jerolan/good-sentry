package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"goodsentry/internal/store"
)

type compatibleInvitationOrgUser struct {
	store.CompatibleOrganizationUser
	Organization store.CompatibleOrganization `json:"organization"`
}

type compatibleInvitationResponse struct {
	AcceptInvite bool                        `json:"acceptInvite"`
	OrgUser      compatibleInvitationOrgUser `json:"orgUser"`
}

func pendingCompatibleInvitation(item store.CompatibleInvitation, accept bool) compatibleInvitationResponse {
	return compatibleInvitationResponse{
		AcceptInvite: accept,
		OrgUser: compatibleInvitationOrgUser{
			CompatibleOrganizationUser: store.CompatibleOrganizationUser{
				ID: item.ID, Role: item.Role, RoleName: invitationRoleName(item.Role), DateCreated: item.CreatedAt.UTC(),
				Email: item.Email, Pending: true, IsOwner: item.Role == "owner", Teams: []string{},
			},
			Organization: item.Organization,
		},
	}
}

func acceptedCompatibleInvitation(item store.CompatibleInvitation, member store.CompatibleOrganizationUser) compatibleInvitationResponse {
	return compatibleInvitationResponse{
		AcceptInvite: true,
		OrgUser:      compatibleInvitationOrgUser{CompatibleOrganizationUser: member, Organization: item.Organization},
	}
}

func (s *Server) compatibleInvitationIdentity(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	if len(r.Header.Values("Authorization")) > 0 {
		var user store.User
		ok := false
		s.compatibleAny(nil, func(w http.ResponseWriter, r *http.Request) {
			user, ok = current(r).User, true
		}).ServeHTTP(w, r)
		return user, ok
	}
	return s.compatibleAccountWithCSRF(w, r, false)
}

func (s *Server) compatibleInvitation(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadCompatibleInvitation(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, pendingCompatibleInvitation(item, false))
}

func (s *Server) loadCompatibleInvitation(w http.ResponseWriter, r *http.Request) (store.CompatibleInvitation, bool) {
	id, token := r.PathValue("invitation"), r.PathValue("token")
	if !validInvitationID(id) || !validInvitationToken(token) {
		failure(w, 403, "Invalid invite token")
		return store.CompatibleInvitation{}, false
	}
	item, err := s.db.CompatibleInvitation(r.Context(), id, s.hash(token))
	if errors.Is(err, store.ErrInvalidInvitation) || errors.Is(err, store.ErrNotFound) {
		failure(w, 403, "Invalid invite token")
		return store.CompatibleInvitation{}, false
	}
	if err != nil {
		report(w, err)
		return store.CompatibleInvitation{}, false
	}
	return item, true
}

func (s *Server) compatibleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		failure(w, 403, "Invalid origin")
		return
	}
	user, ok := s.compatibleInvitationIdentity(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		failure(w, 400, "Invalid JSON request")
		return
	}
	var input struct {
		AcceptInvite *bool `json:"acceptInvite"`
	}
	if err = json.Unmarshal(body, &input); err != nil {
		failure(w, 400, "Invalid JSON request")
		return
	}
	if input.AcceptInvite == nil {
		failure(w, 400, "acceptInvite is required")
		return
	}
	item, ok := s.loadCompatibleInvitation(w, r)
	if !ok {
		return
	}
	if !*input.AcceptInvite {
		writeJSON(w, 200, pendingCompatibleInvitation(item, false))
		return
	}
	if !strings.EqualFold(user.Email, item.Email) {
		byEmail, err := s.db.UserByEmail(r.Context(), item.Email)
		if err != nil || byEmail.ID != user.ID {
			failure(w, 403, "Invalid invite token")
			return
		}
	}
	accepted, org, err := s.db.AcceptInvitation(r.Context(), s.hash(r.PathValue("token")), user.ID, user.AuthGeneration, "", nil)
	if errors.Is(err, store.ErrInvalidInvitation) {
		failure(w, 400, "Invalid or expired invitation")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	member, err := s.db.CompatibleOrganizationMember(r.Context(), accepted.ID, org.Slug, accepted.ID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, acceptedCompatibleInvitation(item, member))
}
