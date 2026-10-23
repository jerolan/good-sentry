package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleManagementRoutes(mux *routeMux) {
	mux.Handle("POST /api/0/organizations/{$}", s.compatible("org:write", s.compatibleCreateOrganization))
	mux.Handle("GET /api/0/organizations/{org}/{$}", s.compatible("org:read", s.compatibleOrganizationDetail))
	mux.Handle("PUT /api/0/organizations/{org}/{$}", s.compatible("org:write", s.compatibleUpdateOrganization))
	mux.Handle("DELETE /api/0/organizations/{org}/{$}", s.compatible("org:write", s.compatibleDeleteOrganization))
	mux.Handle("GET /api/0/projects/{org}/{project}/keys/{$}", s.compatible("project:read", s.compatibleProjectKeys))
	mux.Handle("GET /api/0/projects/{org}/{project}/keys/{key}/{$}", s.compatible("project:read", s.compatibleProjectKey))
	mux.Handle("POST /api/0/projects/{org}/{project}/keys/{$}", s.compatible("project:write", s.compatibleCreateProjectKey))
	mux.Handle("PUT /api/0/projects/{org}/{project}/keys/{key}/{$}", s.compatible("project:write", s.compatibleUpdateProjectKey))
	mux.Handle("DELETE /api/0/projects/{org}/{project}/keys/{key}/{$}", s.compatible("project:write", s.compatibleDeleteProjectKey))
	mux.Handle("GET /api/0/organizations/{org}/members/{$}", s.compatible("org:read", s.compatibleOrganizationMembers))
	mux.Handle("GET /api/0/organizations/{org}/members/{member}/{$}", s.compatible("org:read", s.compatibleOrganizationMember))
	mux.Handle("PUT /api/0/organizations/{org}/members/{member}/{$}", s.compatible("org:write", s.compatibleUpdateOrganizationMember))
	mux.Handle("DELETE /api/0/organizations/{org}/members/{member}/{$}", s.compatible("org:write", s.compatibleDeleteOrganizationMember))
	mux.Handle("POST /api/0/organizations/{org}/members/{member}/teams/{team}/{$}", s.compatible("org:write", s.compatibleAddMemberToTeam))
	mux.Handle("DELETE /api/0/organizations/{org}/members/{member}/teams/{team}/{$}", s.compatible("org:write", s.compatibleRemoveMemberFromTeam))
	mux.Handle("POST /api/0/organizations/{org}/members/{member}/set_owner/{$}", s.compatible("org:write", s.compatibleSetOrganizationOwner))
	mux.Handle("GET /api/0/organizations/{org}/teams/{$}", s.compatible("org:read", s.compatibleOrganizationTeams))
	mux.Handle("GET /api/0/teams/{org}/{team}/{$}", s.compatible("org:read", s.compatibleTeam))
	mux.Handle("GET /api/0/teams/{org}/{team}/members/{$}", s.compatible("org:read", s.compatibleTeamMembers))
	mux.Handle("GET /api/0/teams/{org}/{team}/projects/{$}", s.compatible("org:read", s.compatibleTeamProjects))
	mux.Handle("POST /api/0/teams/{org}/{team}/projects/{$}", s.compatible("org:write", s.compatibleCreateTeamProject))
	mux.Handle("GET /api/0/projects/{org}/{project}/teams/{$}", s.compatible("project:read", s.compatibleProjectTeams))
	mux.Handle("POST /api/0/projects/{org}/{project}/teams/{team}/{$}", s.compatible("project:write", s.compatibleAttachProjectTeam))
	mux.Handle("DELETE /api/0/projects/{org}/{project}/teams/{team}/{$}", s.compatible("project:write", s.compatibleDetachProjectTeam))
	mux.Handle("PUT /api/0/projects/{org}/{project}/{$}", s.compatible("project:write", s.compatibleUpdateProject))
	mux.Handle("DELETE /api/0/projects/{org}/{project}/{$}", s.compatible("project:write", s.compatibleDeleteProject))
	mux.Handle("POST /api/0/organizations/{org}/members/{$}", s.compatible("org:write", s.compatibleCreateOrganizationMember))
	mux.Handle("POST /api/0/organizations/{org}/teams/{$}", s.compatible("org:write", s.compatibleCreateTeam))
	mux.Handle("PUT /api/0/teams/{org}/{team}/{$}", s.compatible("org:write", s.compatibleUpdateTeam))
	mux.Handle("DELETE /api/0/teams/{org}/{team}/{$}", s.compatible("org:write", s.compatibleDeleteTeam))
}

func (s *Server) compatibleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.OrganizationCreation {
		failure(w, 403, "Organization creation is disabled; set ENABLE_ORGANIZATION_CREATION=true")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	authorizationOrg := ""
	if grant, ok := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); ok {
		authorizationOrg = grant.OrganizationID
	}
	organization, err := s.db.CreateCompatibleOrganization(r.Context(), current(r).User.ID, authorizationOrg, input.Name)
	if errors.Is(err, store.ErrInvalidOrganization) {
		failure(w, 400, "Organization name must contain 1 to 200 characters without controls")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, organization)
}

func (s *Server) projectWriteAccess(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return store.ManagementAccess{}, false
	}
	if _, bearer := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); !bearer && access.Role != "owner" && access.Role != "admin" {
		failure(w, 403, "Organization owner or admin permission required")
		return store.ManagementAccess{}, false
	}
	return access, true
}

func (s *Server) compatibleUpdateProject(w http.ResponseWriter, r *http.Request) {
	access, valid := s.projectWriteAccess(w, r)
	if !valid {
		return
	}
	var input struct {
		Name              string `json:"name"`
		Platform          string `json:"platform"`
		EventThrottleRate *int   `json:"eventThrottleRate"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if input.EventThrottleRate != nil && (*input.EventThrottleRate < 0 || *input.EventThrottleRate > 100) {
		failure(w, 400, "eventThrottleRate must be between 0 and 100")
		return
	}
	project, err := s.db.UpdateManagementProject(r.Context(), access, r.PathValue("project"), input.Name, input.Platform, input.EventThrottleRate)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, project)
}

func (s *Server) compatibleDeleteProject(w http.ResponseWriter, r *http.Request) {
	access, valid := s.projectWriteAccess(w, r)
	if !valid {
		return
	}
	if err := s.db.DeleteManagementProject(r.Context(), access, r.PathValue("project")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) compatibleOrganizationDetail(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	detail, err := s.db.CompatibleOrganizationDetail(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	if grant, ok := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); ok && grant.OrganizationID != "" && grant.OrganizationID != detail.ID {
		failure(w, 404, "Not found")
		return
	}
	writeJSON(w, 200, detail)
}

func (s *Server) compatibleUpdateOrganization(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	detail, err := s.db.UpdateCompatibleOrganization(r.Context(), current(r).User.ID, r.PathValue("org"), input.Name)
	if err != nil {
		if errors.Is(err, store.ErrInvalidOrganization) {
			failure(w, 400, "Organization name must contain 1 to 200 characters without controls")
		} else {
			report(w, err)
		}
		return
	}
	writeJSON(w, 200, detail)
}

func (s *Server) compatibleDeleteOrganization(w http.ResponseWriter, r *http.Request) {
	access, valid := s.orgWriteAccess(w, r)
	if !valid {
		return
	}
	if access.Role != "owner" {
		failure(w, 403, "Organization owner permission required")
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	if err := s.db.DeleteCompatibleOrganization(r.Context(), current(r).User.ID, r.PathValue("org")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func pageCompatible[T any](values []T, offset, limit int) ([]T, bool) {
	if offset >= len(values) {
		return []T{}, false
	}
	end := offset + limit
	next := end < len(values)
	if end > len(values) {
		end = len(values)
	}
	return values[offset:end], next
}

func (s *Server) compatibleOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.managementAccess(w, r); !valid {
		return
	}
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	members, err := s.db.CompatibleOrganizationMembers(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	page, next := pageCompatible(members, offset, limit)
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, page)
}

func compatibleMemberPathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.PathValue("member") == "me" {
		return current(r).User.ID, true
	}
	return canonicalPersonID(w, r.PathValue("member"))
}

func compatibleMemberReference(w http.ResponseWriter, r *http.Request) (string, bool, bool) {
	value := r.PathValue("member")
	if value == "me" {
		return current(r).User.ID, false, true
	}
	if validInvitationID(value) {
		return value, true, true
	}
	id, ok := canonicalPersonID(w, value)
	return id, false, ok
}

func (s *Server) compatibleOrganizationMember(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.managementAccess(w, r); !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	memberID, invitation, valid := compatibleMemberReference(w, r)
	if !valid {
		return
	}
	var member store.CompatibleOrganizationUser
	var err error
	if invitation {
		member, err = s.db.CompatibleOrganizationInvitationMember(r.Context(), current(r).User.ID, r.PathValue("org"), memberID)
	} else {
		member, err = s.db.CompatibleOrganizationMember(r.Context(), current(r).User.ID, r.PathValue("org"), memberID)
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, member)
}

func compatibleOrganizationRole(value string) bool {
	return value == "member" || value == "admin" || value == "owner"
}

func (s *Server) compatibleUpdateOrganizationMember(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	memberID, invitation, valid := compatibleMemberReference(w, r)
	if !valid {
		return
	}
	if invitation {
		failure(w, 400, "Pending invitation role changes are not supported")
		return
	}
	var input struct {
		OrgRole   string            `json:"orgRole"`
		TeamRoles []json.RawMessage `json:"teamRoles"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if !compatibleOrganizationRole(input.OrgRole) {
		failure(w, 400, "Organization role must be owner, admin or member")
		return
	}
	if len(input.TeamRoles) > 0 {
		failure(w, 400, "Team roles are not supported yet")
		return
	}
	if _, err := s.db.UpdateMemberRole(r.Context(), current(r).User.ID, r.PathValue("org"), memberID, input.OrgRole); err != nil {
		report(w, err)
		return
	}
	member, err := s.db.CompatibleOrganizationMember(r.Context(), current(r).User.ID, r.PathValue("org"), memberID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, member)
}

func (s *Server) compatibleDeleteOrganizationMember(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	memberID, invitation, valid := compatibleMemberReference(w, r)
	if !valid {
		return
	}
	if invitation {
		if err := s.db.CancelInvitation(r.Context(), current(r).User.ID, r.PathValue("org"), memberID); err != nil {
			report(w, err)
			return
		}
		writeJSON(w, 204, nil)
		return
	}
	if err := s.db.RemoveMember(r.Context(), current(r).User.ID, r.PathValue("org"), memberID); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) compatibleAddMemberToTeam(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	memberID, valid := compatibleMemberPathID(w, r)
	if !valid {
		return
	}
	teamSlug, valid := validTeamSlugInput(w, r.PathValue("team"))
	if !valid {
		return
	}
	team, err := s.db.CompatibleAddTeamMember(r.Context(), current(r).User.ID, r.PathValue("org"), teamSlug, memberID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, team)
}

func (s *Server) compatibleRemoveMemberFromTeam(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	memberID, valid := compatibleMemberPathID(w, r)
	if !valid {
		return
	}
	teamSlug, valid := validTeamSlugInput(w, r.PathValue("team"))
	if !valid {
		return
	}
	team, err := s.db.CompatibleRemoveTeamMember(r.Context(), current(r).User.ID, r.PathValue("org"), teamSlug, memberID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, team)
}

func (s *Server) compatibleSetOrganizationOwner(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	memberID, valid := compatibleMemberPathID(w, r)
	if !valid {
		return
	}
	member, err := s.db.SetCompatibleOrganizationOwner(r.Context(), current(r).User.ID, r.PathValue("org"), memberID)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, member)
}

func (s *Server) orgWriteAccess(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return store.ManagementAccess{}, false
	}
	if _, bearer := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); !bearer && access.Role != "owner" && access.Role != "admin" {
		failure(w, 403, "Organization owner or admin permission required")
		return store.ManagementAccess{}, false
	}
	return access, true
}

type compatibleInvitationView struct {
	ID          string                `json:"id"`
	Role        string                `json:"role"`
	RoleName    string                `json:"roleName"`
	DateCreated time.Time             `json:"dateCreated"`
	Email       string                `json:"email"`
	User        *store.CompatibleUser `json:"user"`
	Pending     bool                  `json:"pending"`
	IsOwner     bool                  `json:"isOwner"`
	InviteLink  *string               `json:"inviteLink"`
}

func invitationRoleName(role string) string {
	switch role {
	case "owner":
		return "Owner"
	case "admin":
		return "Admin"
	default:
		return "Member"
	}
}

func (s *Server) compatibleCreateOrganizationMember(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	var input struct {
		Email      string            `json:"email"`
		OrgRole    string            `json:"orgRole"`
		TeamRoles  []json.RawMessage `json:"teamRoles"`
		SendInvite *bool             `json:"sendInvite"`
		Reinvite   *bool             `json:"reinvite"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if len(input.TeamRoles) > 0 {
		failure(w, 400, "Invitation teamRoles are not supported yet")
		return
	}
	if input.SendInvite != nil && !*input.SendInvite {
		failure(w, 400, "Copy-link-only invitations are not supported yet")
		return
	}
	if input.Reinvite != nil && !*input.Reinvite {
		failure(w, 400, "reinvite=false is not supported yet")
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		failure(w, 400, "Invite a valid email address")
		return
	}
	if input.OrgRole != "owner" && input.OrgRole != "admin" && input.OrgRole != "member" {
		failure(w, 400, "Organization role must be owner, admin or member")
		return
	}
	if s.mail == nil || s.account == nil {
		failure(w, 503, "Invitation email is unavailable")
		return
	}
	token, err := token()
	if err != nil {
		failure(w, 503, "Invitation is temporarily unavailable")
		return
	}
	hash := s.hash(token)
	ciphertext, err := s.account.Seal(hash, token)
	if err != nil {
		failure(w, 503, "Invitation is temporarily unavailable")
		return
	}
	item, err := s.db.InviteMember(r.Context(), current(r).User.ID, r.PathValue("org"), email, input.OrgRole, hash, ciphertext, invitationLifetime)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, compatibleInvitationView{ID: item.ID, Role: item.Role, RoleName: invitationRoleName(item.Role), DateCreated: item.CreatedAt.UTC(), Email: item.Email, Pending: true, IsOwner: item.Role == "owner"})
}

func validTeamSlugInput(w http.ResponseWriter, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 50 || store.Slug(value) != value {
		failure(w, 400, "Team slug must be canonical, lowercase, at most 50 characters")
		return "", false
	}
	return value, true
}

func (s *Server) compatibleCreateTeam(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	var input struct {
		Slug string `json:"slug"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	slug, valid := validTeamSlugInput(w, input.Slug)
	if !valid {
		return
	}
	if _, err := s.db.CreateTeam(r.Context(), current(r).User.ID, r.PathValue("org"), slug); err != nil {
		report(w, err)
		return
	}
	team, err := s.db.CompatibleTeam(r.Context(), current(r).User.ID, r.PathValue("org"), slug)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, team)
}

func (s *Server) compatibleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	var input struct {
		Slug string `json:"slug"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	slug, valid := validTeamSlugInput(w, input.Slug)
	if !valid {
		return
	}
	team, err := s.db.UpdateCompatibleTeam(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("team"), slug)
	if err != nil {
		if errors.Is(err, store.ErrInvalidTeam) {
			failure(w, 400, "Invalid team slug")
		} else {
			report(w, err)
		}
		return
	}
	writeJSON(w, 200, team)
}

func (s *Server) compatibleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	if err := s.db.DeleteCompatibleTeam(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("team")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) compatibleOrganizationTeams(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.managementAccess(w, r); !valid {
		return
	}
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	teams, err := s.db.CompatibleOrganizationTeams(r.Context(), current(r).User.ID, r.PathValue("org"), offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	next := len(teams) > limit
	if next {
		teams = teams[:limit]
	}
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, teams)
}

func (s *Server) compatibleTeam(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.managementAccess(w, r); !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	team, err := s.db.CompatibleTeam(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("team"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, team)
}

func (s *Server) compatibleTeamMembers(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.managementAccess(w, r); !valid {
		return
	}
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	members, err := s.db.CompatibleTeamMembers(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("team"))
	if err != nil {
		report(w, err)
		return
	}
	page, next := pageCompatible(members, offset, limit)
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, page)
}

func (s *Server) compatibleTeamProjects(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.managementAccess(w, r); !valid {
		return
	}
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	projects, err := s.db.CompatibleTeamProjects(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("team"), offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	next := len(projects) > limit
	if next {
		projects = projects[:limit]
	}
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, projects)
}

func (s *Server) compatibleCreateTeamProject(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.orgWriteAccess(w, r); !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	teamSlug, valid := validTeamSlugInput(w, r.PathValue("team"))
	if !valid {
		return
	}
	var input struct {
		Name              string  `json:"name"`
		Slug              *string `json:"slug"`
		Platform          *string `json:"platform"`
		EventThrottleRate *int    `json:"eventThrottleRate"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if input.EventThrottleRate != nil && (*input.EventThrottleRate < 0 || *input.EventThrottleRate > 100) {
		failure(w, 400, "eventThrottleRate must be between 0 and 100")
		return
	}
	var projectSlug, platform string
	if input.Slug != nil {
		projectSlug = *input.Slug
	}
	if input.Platform != nil {
		platform = *input.Platform
	}
	eventThrottleRate := 0
	if input.EventThrottleRate != nil {
		eventThrottleRate = *input.EventThrottleRate
	}
	project, err := s.db.CreateCompatibleProject(r.Context(), current(r).User.ID, r.PathValue("org"), teamSlug, input.Name, projectSlug, platform, eventThrottleRate)
	if errors.Is(err, store.ErrInvalidProject) {
		failure(w, 400, "Project name, slug or platform is invalid")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, project)
}

func (s *Server) compatibleProjectTeams(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	teams, err := s.db.CompatibleProjectTeams(r.Context(), access, r.PathValue("project"))
	if err != nil {
		report(w, err)
		return
	}
	page, next := pageCompatible(teams, offset, limit)
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, page)
}

func (s *Server) compatibleProjectTeam(w http.ResponseWriter, r *http.Request, attach bool) {
	access, valid := s.projectWriteAccess(w, r)
	if !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	teamSlug, valid := validTeamSlugInput(w, r.PathValue("team"))
	if !valid {
		return
	}
	project, err := s.db.CompatibleSetProjectTeam(r.Context(), access, r.PathValue("project"), teamSlug, attach)
	if err != nil {
		report(w, err)
		return
	}
	status := 200
	if attach {
		status = 201
	}
	writeJSON(w, status, project)
}

func (s *Server) compatibleAttachProjectTeam(w http.ResponseWriter, r *http.Request) {
	s.compatibleProjectTeam(w, r, true)
}

func (s *Server) compatibleDetachProjectTeam(w http.ResponseWriter, r *http.Request) {
	s.compatibleProjectTeam(w, r, false)
}

type compatibleDSN struct {
	Secret   string `json:"secret"`
	Public   string `json:"public"`
	CSP      string `json:"csp"`
	Security string `json:"security"`
	Minidump string `json:"minidump"`
	NEL      string `json:"nel"`
	Unreal   string `json:"unreal"`
}

type compatibleProjectKeyView struct {
	store.CompatibleProjectKey
	DSN compatibleDSN `json:"dsn"`
}

func (s *Server) keyDSN(projectID int64, public string) compatibleDSN {
	base := strings.TrimRight(s.cfg.Domain, "/")
	reportURL := base + "/api/" + strconv.FormatInt(projectID, 10) + "/security/?sentry_key=" + url.QueryEscape(public)
	return compatibleDSN{Public: public, CSP: reportURL, Security: reportURL}
}

func (s *Server) keyView(key store.CompatibleProjectKey) compatibleProjectKeyView {
	return compatibleProjectKeyView{CompatibleProjectKey: key, DSN: s.keyDSN(key.ProjectID, key.Public)}
}

func (s *Server) keyAccess(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return store.ManagementAccess{}, false
	}
	if _, bearer := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); !bearer && access.Role != "owner" && access.Role != "admin" {
		failure(w, 403, "Organization owner or admin permission required")
		return store.ManagementAccess{}, false
	}
	return access, true
}

func (s *Server) compatibleProjectKeys(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "status", "limit", "per_page", "cursor") {
		return
	}
	var enabled *bool
	if q.Has("status") && q.Get("status") != "" {
		switch q.Get("status") {
		case "active", "enabled":
			value := true
			enabled = &value
		case "inactive", "disabled":
			value := false
			enabled = &value
		default:
			failure(w, 400, "Unsupported key status")
			return
		}
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	keys, err := s.db.CompatibleProjectKeys(r.Context(), access, r.PathValue("project"), enabled, offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	next := len(keys) > limit
	if next {
		keys = keys[:limit]
	}
	views := make([]compatibleProjectKeyView, 0, len(keys))
	for _, key := range keys {
		views = append(views, s.keyView(key))
	}
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, views)
}

func (s *Server) compatibleProjectKey(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	key, err := s.db.CompatibleProjectKey(r.Context(), access, r.PathValue("project"), r.PathValue("key"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, s.keyView(key))
}

type compatibleProjectKeyInput struct {
	Name      *string                       `json:"name"`
	Label     *string                       `json:"label"`
	RateLimit *store.CompatibleKeyRateLimit `json:"rateLimit"`
}

func keyLabel(w http.ResponseWriter, input compatibleProjectKeyInput) (string, *store.CompatibleKeyRateLimit, bool) {
	if input.Name != nil && input.Label != nil && *input.Name != *input.Label {
		failure(w, 400, "name and label must agree")
		return "", nil, false
	}
	label := ""
	if input.Name != nil {
		label = *input.Name
	} else if input.Label != nil {
		label = *input.Label
	}
	if !credentialText(label, 200) {
		failure(w, 400, "Key label must contain at most 200 characters without controls")
		return "", nil, false
	}
	if input.RateLimit != nil && (input.RateLimit.Window < 1 || input.RateLimit.Window > 604800 || input.RateLimit.Count < 1 || input.RateLimit.Count > 1000000000) {
		failure(w, 400, "Key rate-limit window must be 1..604800 seconds and count 1..1000000000")
		return "", nil, false
	}
	return label, input.RateLimit, true
}

func (s *Server) compatibleCreateProjectKey(w http.ResponseWriter, r *http.Request) {
	access, valid := s.keyAccess(w, r)
	if !valid {
		return
	}
	var input compatibleProjectKeyInput
	if !bodyJSON(w, r, &input) {
		return
	}
	label, rate, valid := keyLabel(w, input)
	if !valid {
		return
	}
	key, err := s.db.CreateCompatibleProjectKey(r.Context(), access, r.PathValue("project"), label, rate)
	if err != nil {
		s.keyInputError(w, err)
		return
	}
	writeJSON(w, 201, s.keyView(key))
}

func (s *Server) compatibleUpdateProjectKey(w http.ResponseWriter, r *http.Request) {
	access, valid := s.keyAccess(w, r)
	if !valid {
		return
	}
	var input compatibleProjectKeyInput
	if !bodyJSON(w, r, &input) {
		return
	}
	label, rate, valid := keyLabel(w, input)
	if !valid {
		return
	}
	key, err := s.db.UpdateCompatibleProjectKey(r.Context(), access, r.PathValue("project"), r.PathValue("key"), label, rate)
	if err != nil {
		s.keyInputError(w, err)
		return
	}
	writeJSON(w, 200, s.keyView(key))
}

func (s *Server) compatibleDeleteProjectKey(w http.ResponseWriter, r *http.Request) {
	access, valid := s.keyAccess(w, r)
	if !valid {
		return
	}
	if err := s.db.DeleteCompatibleProjectKey(r.Context(), access, r.PathValue("project"), r.PathValue("key")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) keyInputError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidProjectKey) {
		failure(w, 400, "Invalid project key input")
		return
	}
	report(w, err)
}
