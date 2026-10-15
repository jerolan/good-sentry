package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"goodsentry/internal/store"
)

func issueResolutionChange(raw json.RawMessage, status string) (*store.IssueResolutionChange, error) {
	if status != "resolved" {
		return nil, errors.New("Resolution requires explicit resolved status")
	}
	change := &store.IssueResolutionChange{}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return change, nil
	}
	fields, err := issueObject(raw, "mode", "version")
	if err != nil {
		return nil, err
	}
	var mode string
	if json.Unmarshal(fields["mode"], &mode) != nil {
		return nil, errors.New("Resolution requires a mode")
	}
	policy := &store.IssueResolutionInput{Mode: mode}
	switch mode {
	case "release":
		var version string
		if len(fields) != 2 || json.Unmarshal(fields["version"], &version) != nil {
			return nil, errors.New("Release resolution requires an exact version string")
		}
		policy.Version, err = store.ParseReleaseVersion(version)
		if err != nil {
			return nil, err
		}
	case "next_release":
		if len(fields) != 1 {
			return nil, errors.New("Next release resolution accepts only mode")
		}
	default:
		return nil, errors.New("Resolution mode must be release or next_release")
	}
	change.Policy = policy
	return change, nil
}

func (s *Server) issueReleases(w http.ResponseWriter, r *http.Request) {
	query, page, ok := issueHistoryQuery(w, r, true)
	if !ok {
		return
	}
	items, total, err := s.db.IssueReleases(r.Context(), current(r).User.ID, r.PathValue("issue"), query, page)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "total": total, "page": page, "pageSize": 25})
}
