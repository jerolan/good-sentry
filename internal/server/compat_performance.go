package server

import (
	"math"
	"net/http"
	"net/url"
	"strconv"

	"goodsentry/internal/store"
)

const compatiblePerformanceLimit = 100

func (s *Server) registerCompatiblePerformanceRoutes(mux *http.ServeMux) {
	read := []string{"event:read", "event:write"}
	mux.Handle("GET /api/0/organizations/{org}/span-groups/{$}", s.compatibleAny(read, s.compatibleSpanGroups))
	mux.Handle("GET /api/0/organizations/{org}/n-plus-one/{$}", s.compatibleAny(read, s.compatibleNPlusOne))
}

func performanceQuery(w http.ResponseWriter, r *http.Request, scalar ...string) (url.Values, bool) {
	query, valid := credentialQuery(w, r)
	if !valid {
		return nil, false
	}
	allowed := map[string]bool{"project": true}
	for _, key := range scalar {
		allowed[key] = true
		if query.Has(key) && len(query[key]) != 1 {
			failure(w, 400, "Performance query parameters must not repeat")
			return nil, false
		}
	}
	for key := range query {
		if !allowed[key] {
			failure(w, 400, "Unsupported performance query parameter")
			return nil, false
		}
	}
	return query, true
}

func performanceProjects(w http.ResponseWriter, query url.Values) ([]int64, bool) {
	values, exists := query["project"]
	if !exists {
		return nil, true
	}
	seen := map[int64]bool{}
	projects := make([]int64, 0, len(values))
	for _, value := range values {
		id, valid := occurrenceNumber(value, math.MaxInt64)
		if !valid || seen[id] {
			failure(w, 400, "Project filters must be distinct positive decimal int64 IDs")
			return nil, false
		}
		seen[id] = true
		projects = append(projects, id)
	}
	return projects, true
}

func performanceLimit(w http.ResponseWriter, query url.Values, fallback int64) (int, bool) {
	if !query.Has("limit") {
		return int(fallback), true
	}
	limit, valid := occurrenceNumber(query.Get("limit"), compatiblePerformanceLimit)
	if !valid {
		failure(w, 400, "Limit must be a positive decimal integer up to 100")
		return 0, false
	}
	return int(limit), true
}

func performanceAccess(s *Server, w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	access, valid := s.managementAccess(w, r)
	if valid {
		access.ReadOnly = true
	}
	return access, valid
}

type compatibleSpanGroup struct {
	Op          string  `json:"op"`
	Description string  `json:"description"`
	Count       int64   `json:"count"`
	AvgDuration float64 `json:"avgDuration"`
	P95Duration float64 `json:"p95Duration"`
	TotalTime   float64 `json:"totalTime"`
}

func (s *Server) compatibleSpanGroups(w http.ResponseWriter, r *http.Request) {
	query, valid := performanceQuery(w, r, "start", "end", "op", "sort", "limit")
	if !valid {
		return
	}
	projects, valid := performanceProjects(w, query)
	if !valid {
		return
	}
	sort := "-total_time"
	if query.Has("sort") {
		sort = query.Get("sort")
		switch sort {
		case "total_time", "-total_time", "avg_duration", "-avg_duration", "count", "-count":
		default:
			failure(w, 400, "Unsupported span group sort")
			return
		}
	}
	limit, valid := performanceLimit(w, query, 50)
	if !valid {
		return
	}
	op := query.Get("op")
	if !credentialText(op, 200) {
		failure(w, 400, "Operation must contain at most 200 characters without controls")
		return
	}
	window, err := queryTimeWindow(query)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	access, valid := performanceAccess(s, w, r)
	if !valid {
		return
	}
	rows, err := s.db.ScopedSpanGroups(r.Context(), access, store.SpanGroupFilter{ProjectIDs: projects, Op: op, Sort: sort, Window: window, Limit: limit})
	if err != nil {
		report(w, err)
		return
	}
	result := make([]compatibleSpanGroup, 0, len(rows))
	for _, row := range rows {
		result = append(result, compatibleSpanGroup{Op: row.Op, Description: row.Description, Count: row.Count, AvgDuration: row.AvgDuration, P95Duration: row.P95Duration, TotalTime: row.TotalTime})
	}
	writeJSON(w, 200, result)
}

func (s *Server) compatibleNPlusOne(w http.ResponseWriter, r *http.Request) {
	query, valid := performanceQuery(w, r, "start", "end", "op", "threshold", "limit")
	if !valid {
		return
	}
	projects, valid := performanceProjects(w, query)
	if !valid {
		return
	}
	limit, valid := performanceLimit(w, query, 50)
	if !valid {
		return
	}
	op := "db"
	if query.Has("op") {
		op = query.Get("op")
	}
	if op == "" || !credentialText(op, 200) {
		failure(w, 400, "Operation must contain 1 to 200 characters without controls")
		return
	}
	threshold := 5
	if query.Has("threshold") {
		value, err := strconv.ParseFloat(query.Get("threshold"), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 2 || value > 100 {
			failure(w, 400, "Threshold must be finite and between 2 and 100")
			return
		}
		threshold = int(math.Ceil(value))
	}
	window, err := queryTimeWindow(query)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	access, valid := performanceAccess(s, w, r)
	if !valid {
		return
	}
	rows, err := s.db.ScopedNPlusOnePatterns(r.Context(), access, store.NPlusOneFilter{ProjectIDs: projects, Op: op, Window: window, Threshold: threshold, Limit: limit})
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, rows)
}
