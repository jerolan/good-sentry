package server

import (
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"goodsentry/internal/eventview"
	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleIssueHashRoutes(mux *http.ServeMux) {
	const path = "/api/0/organizations/{org}/issues/{issue}/hashes/{$}"
	mux.Handle("GET "+path, s.compatibleAny([]string{"event:read", "event:write"}, s.compatibleIssueHashes))
	mux.Handle("DELETE "+path, s.compatible("event:write", s.compatibleIssueHashReset))
}

type compatibleIssueHashPaging struct {
	query  url.Values
	filter store.CompatibleIssueHashFilter
	full   bool
}

func compatibleIssueHashListQuery(w http.ResponseWriter, r *http.Request) (compatibleIssueHashPaging, bool) {
	paging := compatibleIssueHashPaging{filter: store.CompatibleIssueHashFilter{Limit: 100}, full: true}
	query, ok := compatibleIssueQuery(w, r, []string{"limit", "cursor", "full"}, nil)
	if !ok {
		return paging, false
	}
	paging.query = query
	if query.Has("limit") {
		limit, valid := occurrenceNumber(query.Get("limit"), 100)
		if !valid {
			failure(w, 400, "limit must be a positive decimal integer up to 100")
			return paging, false
		}
		paging.filter.Limit = int(limit)
	}
	if query.Has("full") {
		switch query.Get("full") {
		case "true", "1":
			paging.full = true
		case "false", "0":
			paging.full = false
		default:
			failure(w, 400, "full must be true, false, 1 or 0")
			return paging, false
		}
	}
	if paging.full {
		paging.filter.Limit = min(10, paging.filter.Limit)
	}
	if cursor := query.Get("cursor"); cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 3 || parts[0] != "0" || parts[2] != "0" && parts[2] != "1" {
			failure(w, 400, "Cursor is invalid")
			return paging, false
		}
		offset, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || offset < 0 || offset > maxProjectOffset || strconv.FormatInt(offset, 10) != parts[1] {
			failure(w, 400, "Cursor is invalid")
			return paging, false
		}
		paging.filter.Offset = int(offset)
	}
	return paging, true
}

func compatibleIssueHashResetQuery(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	query, ok := credentialQuery(w, r)
	if !ok {
		return nil, false
	}
	if len(query) != 1 || len(query["id"]) < 1 || len(query["id"]) > 1000 {
		failure(w, 400, "Reset requires between 1 and 1000 id values and no other query parameters")
		return nil, false
	}
	ids := make([]string, 0, len(query["id"]))
	seen := make(map[string]bool, len(query["id"]))
	for _, value := range query["id"] {
		if len(value) == 36 && value[8] == '-' && value[13] == '-' && value[18] == '-' && value[23] == '-' {
			value = value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
		}
		if len(value) != 32 {
			failure(w, 400, "Hash IDs must be 32-digit hexadecimal or canonical hyphenated UUIDs")
			return nil, false
		}
		if _, err := hex.DecodeString(value); err != nil {
			failure(w, 400, "Hash IDs must be 32-digit hexadecimal or canonical hyphenated UUIDs")
			return nil, false
		}
		value = strings.ToLower(value)
		if !seen[value] {
			seen[value] = true
			ids = append(ids, value)
		}
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			failure(w, 400, "Hash reset requires an empty request body")
			return nil, false
		}
	}
	return ids, true
}

type compatibleIssueHashView struct {
	ID                string           `json:"id"`
	LatestEvent       *eventview.Event `json:"latestEvent"`
	MergedBySeer      bool             `json:"mergedBySeer"`
	SeerMatchDistance *float64         `json:"seerMatchDistance"`
}

func (s *Server) compatibleIssueHashes(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	if !issueTargetID(r.PathValue("issue")) {
		failure(w, 400, "Issue IDs must be canonical positive decimal int64 strings")
		return
	}
	paging, ok := compatibleIssueHashListQuery(w, r)
	if !ok {
		return
	}
	records, err := s.db.CompatibleIssueHashes(r.Context(), access, r.PathValue("issue"), paging.filter)
	if err != nil {
		reportCompatibleIssue(w, err)
		return
	}
	next := len(records) > paging.filter.Limit
	if next {
		records = records[:paging.filter.Limit]
	}
	result := make([]compatibleIssueHashView, 0, len(records))
	for _, record := range records {
		item := compatibleIssueHashView{ID: record.ID}
		if record.Latest != nil {
			sample, err := eventview.Project(*record.Latest, paging.full)
			if err != nil {
				report(w, err)
				return
			}
			item.LatestEvent = &sample
		}
		result = append(result, item)
	}
	s.compatibleOffsetLinks(w, r, paging.query, paging.filter.Offset, paging.filter.Limit, next)
	writeJSON(w, 200, result)
}

func (s *Server) compatibleIssueHashReset(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	access.ReadOnly = false
	if !issueTargetID(r.PathValue("issue")) {
		failure(w, 400, "Issue IDs must be canonical positive decimal int64 strings")
		return
	}
	ids, ok := compatibleIssueHashResetQuery(w, r)
	if !ok {
		return
	}
	if err := s.db.ResetCompatibleIssueHashes(r.Context(), access, r.PathValue("issue"), ids); err != nil {
		if errors.Is(err, store.ErrInvalidCompatibleIssueFilter) {
			reportCompatibleIssue(w, err)
		} else {
			reportCompatibleIssueChange(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
