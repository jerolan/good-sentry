package server

import (
	"math"
	"net/http"
	"strings"
)

func canonicalPersonID(w http.ResponseWriter, value string) (string, bool) {
	if _, valid := occurrenceNumber(value, math.MaxInt64); !valid {
		failure(w, 400, "Member and team IDs must be canonical positive decimal integers")
		return "", false
	}
	return value, true
}

func validTeamName(w http.ResponseWriter, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !credentialText(value, 120) {
		failure(w, 400, "Team name must contain 1 to 120 characters without controls")
		return "", false
	}
	return value, true
}

func (s *Server) members(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Members(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (s *Server) updateMemberRole(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalPersonID(w, r.PathValue("member"))
	if !ok {
		return
	}
	var input struct {
		Role string `json:"role"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if input.Role != "owner" && input.Role != "manager" && input.Role != "admin" && input.Role != "member" {
		failure(w, 400, "Role must be owner, manager, admin or member")
		return
	}
	member, err := s.db.UpdateMemberRole(r.Context(), current(r).User.ID, r.PathValue("org"), id, input.Role)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, member)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalPersonID(w, r.PathValue("member"))
	if !ok {
		return
	}
	if err := s.db.RemoveMember(r.Context(), current(r).User.ID, r.PathValue("org"), id); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) teams(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.Teams(r.Context(), current(r).User.ID, r.PathValue("org"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (s *Server) team(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalPersonID(w, r.PathValue("team"))
	if !ok {
		return
	}
	item, err := s.db.Team(r.Context(), current(r).User.ID, r.PathValue("org"), id)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	name, ok := validTeamName(w, input.Name)
	if !ok {
		return
	}
	item, err := s.db.CreateTeam(r.Context(), current(r).User.ID, r.PathValue("org"), name)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) updateTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalPersonID(w, r.PathValue("team"))
	if !ok {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	name, ok := validTeamName(w, input.Name)
	if !ok {
		return
	}
	item, err := s.db.UpdateTeam(r.Context(), current(r).User.ID, r.PathValue("org"), id, name)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) deleteTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalPersonID(w, r.PathValue("team"))
	if !ok {
		return
	}
	if err := s.db.DeleteTeam(r.Context(), current(r).User.ID, r.PathValue("org"), id); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) addTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID, ok := canonicalPersonID(w, r.PathValue("team"))
	if !ok {
		return
	}
	memberID, ok := canonicalPersonID(w, r.PathValue("member"))
	if !ok {
		return
	}
	if err := s.db.AddTeamMember(r.Context(), current(r).User.ID, r.PathValue("org"), teamID, memberID); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) removeTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID, ok := canonicalPersonID(w, r.PathValue("team"))
	if !ok {
		return
	}
	memberID, ok := canonicalPersonID(w, r.PathValue("member"))
	if !ok {
		return
	}
	if err := s.db.RemoveTeamMember(r.Context(), current(r).User.ID, r.PathValue("org"), teamID, memberID); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}
