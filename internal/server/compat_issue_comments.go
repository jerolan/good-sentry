package server

import (
	"net/http"
	"time"

	"goodsentry/internal/store"
)

type compatibleComment struct {
	ID          string                `json:"id"`
	Type        string                `json:"type"`
	Data        map[string]string     `json:"data"`
	DateCreated string                `json:"dateCreated"`
	User        compatibleCommentUser `json:"user"`
}

type compatibleCommentUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type compatibleCommentInput struct {
	Data struct {
		Text string `json:"text"`
	} `json:"data"`
}

func commentView(comment store.Comment) compatibleComment {
	return compatibleComment{
		ID: comment.ID, Type: "note", Data: map[string]string{"text": comment.Text},
		DateCreated: comment.DateCreated.UTC().Format(time.RFC3339Nano),
		User:        compatibleCommentUser{ID: comment.User.ID, Email: comment.User.Email},
	}
}

func (s *Server) compatibleIssueVisible(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return store.ManagementAccess{}, false
	}
	if _, err := s.db.CompatibleIssue(r.Context(), access, r.PathValue("issue"), store.CompatibleIssueDetailFilter{StatsPeriod: "24h"}); err != nil {
		reportCompatibleIssue(w, err)
		return store.ManagementAccess{}, false
	}
	return access, true
}

func (s *Server) compatibleIssueComments(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.compatibleIssueVisible(w, r); !ok {
		return
	}
	q, ok := credentialQuery(w, r)
	if !ok || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, ok := discoveryPage(w, q)
	if !ok {
		return
	}
	comments, total, err := s.db.IssueCommentsPage(r.Context(), current(r).User.ID, r.PathValue("issue"), offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	result := make([]compatibleComment, 0, len(comments))
	for _, comment := range comments {
		result = append(result, commentView(comment))
	}
	s.discoveryLinks(w, r, q, offset, limit, offset+len(result) < total)
	writeJSON(w, 200, result)
}

func (s *Server) compatibleAddIssueComment(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.compatibleIssueVisible(w, r); !ok {
		return
	}
	var input compatibleCommentInput
	if !bodyJSON(w, r, &input) {
		return
	}
	comment, err := s.db.AddComment(r.Context(), current(r).User.ID, r.PathValue("issue"), input.Data.Text)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, commentView(comment))
}

func (s *Server) compatibleEditIssueComment(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.compatibleIssueVisible(w, r); !ok {
		return
	}
	var input compatibleCommentInput
	if !bodyJSON(w, r, &input) {
		return
	}
	comment, err := s.db.EditComment(r.Context(), current(r).User.ID, r.PathValue("issue"), r.PathValue("comment"), input.Data.Text)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, commentView(comment))
}

func (s *Server) compatibleDeleteIssueComment(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.compatibleIssueVisible(w, r); !ok {
		return
	}
	if err := s.db.DeleteComment(r.Context(), current(r).User.ID, r.PathValue("issue"), r.PathValue("comment")); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 204, nil)
}
