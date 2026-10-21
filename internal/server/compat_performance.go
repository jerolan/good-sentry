package server

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"goodsentry/internal/store"
)

const compatiblePerformanceLimit = 100

func (s *Server) registerCompatiblePerformanceRoutes(mux *http.ServeMux) {
	read := []string{"event:read", "event:write"}
	mux.Handle("GET /api/0/organizations/{org}/transaction-groups/{$}", s.compatibleAny(read, s.compatibleTransactionGroups))
	mux.Handle("GET /api/0/organizations/{org}/transaction-groups/{id}/{$}", s.compatibleAny(read, s.compatibleTransactionGroup))
	mux.Handle("GET /api/0/organizations/{org}/transaction-groups/{id}/spans/{$}", s.compatibleAny(read, s.compatibleTransactionGroupSpans))
	mux.Handle("GET /api/0/organizations/{org}/transaction-groups/{id}/trend/{$}", s.compatibleAny(read, s.compatibleTransactionGroupTrend))
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

type compatibleTransactionGroup struct {
	Project     int64     `json:"project"`
	ID          int64     `json:"id"`
	Transaction string    `json:"transaction"`
	Op          string    `json:"op"`
	Method      string    `json:"method"`
	Count       int64     `json:"count"`
	AvgDuration float64   `json:"avgDuration"`
	P50         float64   `json:"p50"`
	P95         float64   `json:"p95"`
	ErrorCount  int64     `json:"errorCount"`
	FirstSeen   time.Time `json:"firstSeen"`
	LastSeen    time.Time `json:"lastSeen"`
	ErrorRate   float64   `json:"errorRate"`
	Throughput  float64   `json:"throughput"`
}

func transactionGroupView(group store.CompatibleTransactionGroup, window *store.TimeWindow) compatibleTransactionGroup {
	minutes := group.LastSeen.Sub(group.FirstSeen).Minutes()
	if window != nil {
		minutes = window.End.Sub(window.Start).Minutes()
	}
	if minutes < 1 {
		minutes = 1
	}
	errorRate := 0.0
	if group.Count > 0 {
		errorRate = float64(group.ErrorCount) / float64(group.Count)
	}
	return compatibleTransactionGroup{
		Project: group.ProjectID, ID: group.ID, Transaction: group.Transaction, Op: group.Op, Method: group.Method,
		Count: group.Count, AvgDuration: group.AvgDuration, P50: group.P50, P95: group.P95, ErrorCount: group.ErrorCount,
		FirstSeen: group.FirstSeen.UTC(), LastSeen: group.LastSeen.UTC(), ErrorRate: errorRate,
		Throughput: float64(group.Count) / minutes,
	}
}

func (s *Server) compatibleTransactionGroups(w http.ResponseWriter, r *http.Request) {
	query, valid := performanceQuery(w, r, "start", "end", "sort", "query", "limit", "cursor")
	if !valid {
		return
	}
	projects, valid := performanceProjects(w, query)
	if !valid {
		return
	}
	sort := "-avg_duration"
	if query.Has("sort") {
		sort = query.Get("sort")
		switch sort {
		case "created", "-created", "avg_duration", "-avg_duration", "count", "-count":
		default:
			failure(w, 400, "Unsupported transaction group sort")
			return
		}
	}
	search := query.Get("query")
	if !credentialText(search, 2000) {
		failure(w, 400, "Query must contain at most 2000 characters without NUL")
		return
	}
	offset, limit, valid := discoveryPage(w, query)
	if !valid {
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
	groups, err := s.db.CompatibleTransactionGroups(r.Context(), access, store.CompatibleTransactionFilter{
		ProjectIDs: projects, Query: search, Sort: sort, Window: window, Offset: offset, Limit: limit + 1,
	})
	if err != nil {
		report(w, err)
		return
	}
	next := len(groups) > limit
	if next {
		groups = groups[:limit]
	}
	result := make([]compatibleTransactionGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, transactionGroupView(group, window))
	}
	s.discoveryLinks(w, r, query, offset, limit, next)
	writeJSON(w, 200, result)
}

func compatibleTransactionGroupResponse(s *Server, w http.ResponseWriter, r *http.Request) (store.CompatibleTransactionIdentity, bool) {
	id, valid := occurrenceNumber(r.PathValue("id"), math.MaxInt64)
	if !valid {
		failure(w, 400, "Transaction group ID must be a positive decimal int64")
		return store.CompatibleTransactionIdentity{}, false
	}
	access, valid := performanceAccess(s, w, r)
	if !valid {
		return store.CompatibleTransactionIdentity{}, false
	}
	identity, err := s.db.CompatibleTransactionGroupIdentity(r.Context(), access, id)
	if err != nil {
		report(w, err)
		return store.CompatibleTransactionIdentity{}, false
	}
	return identity, true
}

func (s *Server) compatibleTransactionGroup(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	identity, valid := compatibleTransactionGroupResponse(s, w, r)
	if !valid {
		return
	}
	access, _ := performanceAccess(s, w, r)
	group, err := s.db.CompatibleTransactionGroup(r.Context(), access, identity, nil)
	if errors.Is(err, store.ErrNotFound) {
		group = store.CompatibleTransactionGroup{ProjectID: identity.ProjectID, Transaction: identity.Transaction, Op: identity.Op, FirstSeen: identity.CreatedAt, LastSeen: identity.CreatedAt}
		identityID, _ := occurrenceNumber(r.PathValue("id"), math.MaxInt64)
		group.ID = identityID
		err = nil
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, transactionGroupView(group, nil))
}

type compatibleTransactionTrend struct {
	Date             time.Time `json:"date"`
	Count            int64     `json:"count"`
	TransactionCount int64     `json:"transactionCount"`
	AvgDuration      float64   `json:"avgDuration"`
	TotalTime        float64   `json:"totalTime"`
}

func (s *Server) compatibleTransactionGroupTrend(w http.ResponseWriter, r *http.Request) {
	query, valid := performanceQuery(w, r, "start", "end")
	if !valid {
		return
	}
	if query.Has("project") {
		failure(w, 400, "Project filters are not accepted on transaction group detail routes")
		return
	}
	window, err := queryTimeWindow(query)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	identity, valid := compatibleTransactionGroupResponse(s, w, r)
	if !valid {
		return
	}
	access, _ := performanceAccess(s, w, r)
	points, err := s.db.ScopedTransactionTrend(r.Context(), access, store.TransactionFilter{
		Project: strconv.FormatInt(identity.ProjectID, 10), Transaction: identity.Transaction, Op: identity.Op, Window: window,
	})
	if err != nil {
		report(w, err)
		return
	}
	result := make([]compatibleTransactionTrend, 0, len(points))
	for _, point := range points {
		day, _ := time.Parse("2006-01-02", point.Date)
		result = append(result, compatibleTransactionTrend{
			Date: day.UTC(), Count: point.Count, TransactionCount: point.Count,
			AvgDuration: point.AvgDuration, TotalTime: point.AvgDuration * float64(point.Count),
		})
	}
	writeJSON(w, 200, result)
}

func (s *Server) compatibleTransactionGroupSpans(w http.ResponseWriter, r *http.Request) {
	query, valid := performanceQuery(w, r, "start", "end")
	if !valid {
		return
	}
	if query.Has("project") {
		failure(w, 400, "Project filters are not accepted on transaction group detail routes")
		return
	}
	window, err := queryTimeWindow(query)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	identity, valid := compatibleTransactionGroupResponse(s, w, r)
	if !valid {
		return
	}
	access, _ := performanceAccess(s, w, r)
	group, err := s.db.ScopedTransactionGroup(r.Context(), access, store.TransactionFilter{
		Project: strconv.FormatInt(identity.ProjectID, 10), Transaction: identity.Transaction, Op: identity.Op,
	})
	if err != nil {
		report(w, err)
		return
	}
	spans, err := s.db.TransactionSpanGroups(r.Context(), current(r).User.ID, r.PathValue("org"), group.LatestSample.Source, group.LatestSample.ID, group.LatestSample.ProjectID, window, 1)
	if err != nil {
		report(w, err)
		return
	}
	result := make([]compatibleSpanGroup, 0, len(spans.Data))
	for _, span := range spans.Data {
		average, p95, total := 0.0, 0.0, 0.0
		if span.AvgDuration != nil {
			average = *span.AvgDuration
		}
		if span.P95Duration != nil {
			p95 = *span.P95Duration
		}
		if span.TotalTime != nil {
			total = *span.TotalTime
		}
		result = append(result, compatibleSpanGroup{Op: span.Op, Description: span.Description, Count: span.Count, AvgDuration: average, P95Duration: p95, TotalTime: total})
	}
	writeJSON(w, 200, result)
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
