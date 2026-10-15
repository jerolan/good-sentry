package server

import (
	"encoding/json"
	"net/http"
	"net/url"
)

func issueGroupIDs(w http.ResponseWriter, r *http.Request, field string, minimum int) ([]string, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != 0 {
		failure(w, 400, "Grouping mutations do not accept query parameters")
		return nil, false
	}
	fields, ok := issueBody(w, r, field)
	if !ok {
		return nil, false
	}
	var ids []string
	if len(fields) != 1 || json.Unmarshal(fields[field], &ids) != nil || len(ids) < minimum || len(ids) > 100 {
		failure(w, 400, "Provide a bounded array of distinct positive ID strings")
		return nil, false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !issueTargetID(id) || seen[id] {
			failure(w, 400, "IDs must be distinct canonical positive int64 strings")
			return nil, false
		}
		seen[id] = true
	}
	return ids, true
}

func (s *Server) mergeIssues(w http.ResponseWriter, r *http.Request) {
	ids, ok := issueGroupIDs(w, r, "issueIds", 2)
	if !ok {
		return
	}
	result, err := s.db.MergeIssues(r.Context(), current(r).User.ID, r.PathValue("org"), ids)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) unmergeIssue(w http.ResponseWriter, r *http.Request) {
	ids, ok := issueGroupIDs(w, r, "originIds", 1)
	if !ok {
		return
	}
	result, err := s.db.UnmergeIssue(r.Context(), current(r).User.ID, r.PathValue("issue"), ids)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) issueOrigins(w http.ResponseWriter, r *http.Request) {
	query, page, ok := issueHistoryQuery(w, r, true)
	if !ok {
		return
	}
	result, err := s.db.IssueOrigins(r.Context(), current(r).User.ID, r.PathValue("issue"), query, page)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, result)
}
