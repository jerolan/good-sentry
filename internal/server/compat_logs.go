package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/store"
)

const (
	compatibleLogDefaultLimit  = 100
	compatibleLogStatsDefaultH = 7 * 24
	compatibleLogStatsMaxH     = store.CompatibleLogStatsHours
)

// compatibleLogSeverity inverts the ingest mapping so a client can read the
// OpenTelemetry severity back from a retained level.
var compatibleLogSeverity = map[string]int{"trace": 1, "debug": 5, "info": 9, "warning": 13, "error": 17, "fatal": 21}

var compatibleLogLevelOrder = []string{"fatal", "error", "warning", "info", "debug", "trace"}

func (s *Server) registerCompatibleLogRoutes(mux *http.ServeMux) {
	root := "/api/0/organizations/{org}/logs/"
	scopes := []string{"event:read", "event:write"}
	mux.Handle("GET "+root+"{$}", s.compatibleAny(scopes, s.compatibleLogList))
	mux.Handle("GET "+root+"resources/{$}", s.compatibleAny(scopes, s.compatibleLogResources))
	mux.Handle("GET "+root+"stats/{$}", s.compatibleAny(scopes, s.compatibleLogStats))
	mux.Handle("GET "+root+"{log}/{$}", s.compatibleAny(scopes, s.compatibleLogDetail))
}

// compatibleLogQuery admits the published log query surface. Repeated values are
// accepted only for the keys the route publishes as arrays.
func compatibleLogQuery(w http.ResponseWriter, r *http.Request, repeatable ...string) (url.Values, bool) {
	query, valid := credentialQuery(w, r)
	if !valid {
		return nil, false
	}
	allowed := map[string]bool{
		"project": true, "level": true, "service": true, "environment": true, "host": true,
		"traceId": true, "query": true, "start": true, "end": true, "cursor": true, "limit": true,
	}
	repeats := map[string]bool{"project": true}
	for _, key := range repeatable {
		repeats[key] = true
	}
	for key := range query {
		if !allowed[key] {
			failure(w, 400, "Unsupported log query parameter")
			return nil, false
		}
		if !repeats[key] && len(query[key]) != 1 {
			failure(w, 400, "Log query parameters must not repeat")
			return nil, false
		}
	}
	return query, true
}

func compatibleLogText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= 2000
}

func compatibleLogLevels(query url.Values) ([]string, bool) {
	values := query["level"]
	levels := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" {
			continue
		}
		published := store.PublishedCompatibleLevel(value)
		if published == "warn" {
			published = "warning"
		}
		if _, exists := compatibleLogSeverity[published]; !exists {
			return nil, false
		}
		if seen[published] {
			continue
		}
		seen[published] = true
		levels = append(levels, published)
	}
	return levels, true
}

// compatibleTraceID accepts the published UUID spelling and the canonical
// thirty-two digit form, and returns the lowercase hexadecimal identity.
func compatibleTraceID(value string) (string, bool) {
	compact := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "")
	if len(compact) != 32 {
		return "", false
	}
	for index := 0; index < len(compact); index++ {
		if c := compact[index]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	return compact, true
}

func compatibleLogLimit(query url.Values) (int, bool) {
	if !query.Has("limit") {
		return compatibleLogDefaultLimit, true
	}
	limit, valid := occurrenceNumber(query.Get("limit"), store.CompatibleLogMaxLimit)
	return int(limit), valid
}

func compatibleLogCursor(query url.Values) (store.CompatibleLogCursor, bool) {
	value := query.Get("cursor")
	if value == "" {
		return store.CompatibleLogCursor{}, true
	}
	parts := strings.Split(value, "|")
	if len(parts) != 2 {
		return store.CompatibleLogCursor{}, false
	}
	stamp, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return store.CompatibleLogCursor{}, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return store.CompatibleLogCursor{}, false
	}
	return store.CompatibleLogCursor{Timestamp: stamp.UTC(), ID: id}, true
}

func compatibleLogFilter(w http.ResponseWriter, r *http.Request, query url.Values, stats bool) (store.CompatibleLogFilter, bool) {
	projects, valid := performanceProjects(w, query)
	if !valid {
		return store.CompatibleLogFilter{}, false
	}
	levels, valid := compatibleLogLevels(query)
	if !valid {
		failure(w, 400, "Levels must be trace, debug, info, warning, error or fatal")
		return store.CompatibleLogFilter{}, false
	}
	for _, key := range []string{"service", "environment", "host"} {
		if !compatibleLogText(query.Get(key)) {
			failure(w, 400, "Filters must be valid UTF-8 without NUL and at most 2000 characters")
			return store.CompatibleLogFilter{}, false
		}
	}
	if !compatibleLogText(query.Get("query")) {
		failure(w, 400, "Search text must be valid UTF-8 without NUL and at most 2000 characters")
		return store.CompatibleLogFilter{}, false
	}
	f := store.CompatibleLogFilter{ProjectIDs: projects, Levels: levels, Query: query.Get("query"), Limit: compatibleLogDefaultLimit}
	if !stats {
		f.Service, f.Environment, f.Host = query.Get("service"), query.Get("environment"), query.Get("host")
	} else {
		f.Services = query["service"]
		f.Environments = query["environment"]
		for _, value := range append(append([]string{}, f.Services...), f.Environments...) {
			if !compatibleLogText(value) {
				failure(w, 400, "Filters must be valid UTF-8 without NUL and at most 2000 characters")
				return store.CompatibleLogFilter{}, false
			}
		}
	}
	if query.Has("traceId") {
		trace, valid := compatibleTraceID(query.Get("traceId"))
		if !valid {
			failure(w, 400, "Trace ID must be a 32-digit hexadecimal or UUID value")
			return store.CompatibleLogFilter{}, false
		}
		f.TraceID = trace
	}
	window, err := queryTimeWindow(query)
	if err != nil {
		failure(w, 400, err.Error())
		return store.CompatibleLogFilter{}, false
	}
	if stats {
		if window == nil {
			end := time.Now().UTC().Truncate(time.Microsecond)
			window = &store.TimeWindow{Start: end.Add(-compatibleLogStatsDefaultH * time.Hour), End: end}
		}
		if window.End.Sub(window.Start) > compatibleLogStatsMaxH*time.Hour {
			failure(w, 400, "Statistics windows are capped at 90 days")
			return store.CompatibleLogFilter{}, false
		}
		f.Window = window
		return f, true
	}
	f.Window = window
	limit, valid := compatibleLogLimit(query)
	if !valid {
		failure(w, 400, "Limit must be a positive decimal integer up to 200")
		return store.CompatibleLogFilter{}, false
	}
	f.Limit = limit
	cursor, valid := compatibleLogCursor(query)
	if !valid {
		failure(w, 400, "Cursor is invalid")
		return store.CompatibleLogFilter{}, false
	}
	f.Cursor = cursor
	return f, true
}

// compatibleLogResponse is the published GlitchTip log shape.
type compatibleLogResponse struct {
	ID             string         `json:"id"`
	Timestamp      time.Time      `json:"timestamp"`
	Level          string         `json:"level"`
	Body           string         `json:"body"`
	Service        string         `json:"service"`
	Environment    string         `json:"environment"`
	Host           string         `json:"host"`
	TraceID        string         `json:"traceID"`
	SpanID         string         `json:"spanID"`
	SeverityNumber *int           `json:"severityNumber"`
	Data           map[string]any `json:"data"`
	ProjectID      string         `json:"projectId"`
}

// compatibleLogData unwraps the typed Sentry attribute envelope into the plain
// object the published shape carries, keeping unknown shapes untouched.
func compatibleLogData(attributes json.RawMessage) map[string]any {
	data := map[string]any{}
	if len(attributes) == 0 {
		return data
	}
	var decoded map[string]any
	if err := json.Unmarshal(attributes, &decoded); err != nil {
		return data
	}
	for name, value := range decoded {
		if typed, ok := value.(map[string]any); ok {
			if _, hasType := typed["type"]; hasType {
				if unwrapped, hasValue := typed["value"]; hasValue {
					data[name] = unwrapped
					continue
				}
			}
		}
		data[name] = value
	}
	return data
}

func compatibleLogView(log store.CompatibleLog) compatibleLogResponse {
	view := compatibleLogResponse{
		ID: log.ID, Timestamp: log.Timestamp.UTC(), Level: log.Level, Body: log.Body,
		Service: log.Service, Environment: log.Environment, Host: log.Host,
		TraceID: log.TraceID, SpanID: log.SpanID, Data: compatibleLogData(log.Attributes),
		ProjectID: log.ProjectID,
	}
	if severity, ok := compatibleLogSeverity[log.Level]; ok {
		view.SeverityNumber = &severity
	}
	return view
}

func (s *Server) compatibleLogAccess(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return store.ManagementAccess{}, false
	}
	access.ReadOnly = true
	return access, true
}

func (s *Server) compatibleLogList(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs {
		failure(w, 404, "Log ingestion is disabled")
		return
	}
	query, valid := compatibleLogQuery(w, r, "level")
	if !valid {
		return
	}
	f, valid := compatibleLogFilter(w, r, query, false)
	if !valid {
		return
	}
	access, valid := s.compatibleLogAccess(w, r)
	if !valid {
		return
	}
	page, err := s.db.CompatibleLogs(r.Context(), access, f)
	if err != nil {
		if errors.Is(err, store.ErrInvalidEventFilter) {
			failure(w, 400, "Invalid log filters")
			return
		}
		report(w, err)
		return
	}
	items := make([]compatibleLogResponse, 0, len(page))
	for _, item := range page {
		items = append(items, compatibleLogView(item))
	}
	if cursor, more := store.CompatibleLogCursorFrom(page); more && len(page) == f.Limit {
		public := strings.TrimRight(s.cfg.Domain, "/") + r.URL.EscapedPath() + "?" + compatibleLogNextQuery(query, cursor).Encode()
		w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"next\"; results=\"true\"; cursor=\"%s|%d\"", public, cursor.Timestamp.UTC().Format(time.RFC3339Nano), cursor.ID))
	}
	writeJSON(w, 200, items)
}

func compatibleLogNextQuery(query url.Values, cursor store.CompatibleLogCursor) url.Values {
	next := url.Values{}
	for key, values := range query {
		if key == "cursor" {
			continue
		}
		for _, value := range values {
			next.Add(key, value)
		}
	}
	next.Set("cursor", cursor.Timestamp.UTC().Format(time.RFC3339Nano)+"|"+strconv.FormatInt(cursor.ID, 10))
	return next
}

func (s *Server) compatibleLogDetail(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs {
		failure(w, 404, "Log ingestion is disabled")
		return
	}
	if _, valid := occurrenceNumber(r.PathValue("log"), math.MaxInt64); !valid {
		failure(w, 400, "Log ID must be a positive decimal int64")
		return
	}
	if r.URL.RawQuery != "" {
		failure(w, 400, "Log detail accepts no query parameters")
		return
	}
	access, valid := s.compatibleLogAccess(w, r)
	if !valid {
		return
	}
	log, err := s.db.CompatibleLog(r.Context(), access, r.PathValue("log"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			failure(w, 404, "Log not found")
			return
		}
		report(w, err)
		return
	}
	writeJSON(w, 200, compatibleLogView(log))
}

func (s *Server) compatibleLogResources(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs {
		failure(w, 404, "Log ingestion is disabled")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Query encoding is invalid")
		return
	}
	for key := range query {
		if key != "resource_type" || len(query[key]) != 1 {
			failure(w, 400, "Only one resource_type filter is supported")
			return
		}
	}
	resourceType := query.Get("resource_type")
	switch resourceType {
	case "", "service", "environment", "host":
	default:
		failure(w, 400, "Resource type must be service, environment or host")
		return
	}
	access, valid := s.compatibleLogAccess(w, r)
	if !valid {
		return
	}
	resources, err := s.db.CompatibleLogResources(r.Context(), access, resourceType)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, resources)
}

func (s *Server) compatibleLogStats(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Logs {
		failure(w, 404, "Log ingestion is disabled")
		return
	}
	query, valid := compatibleLogQuery(w, r, "level", "service", "environment")
	if !valid {
		return
	}
	f, valid := compatibleLogFilter(w, r, query, true)
	if !valid {
		return
	}
	access, valid := s.compatibleLogAccess(w, r)
	if !valid {
		return
	}
	stats, err := s.db.CompatibleLogStats(r.Context(), access, f)
	if err != nil {
		if errors.Is(err, store.ErrInvalidEventFilter) {
			failure(w, 400, "Invalid statistics filters")
			return
		}
		report(w, err)
		return
	}
	writeJSON(w, 200, compatibleLogStatsResponse(f.Window, stats))
}

type compatibleLogStatsSeries struct {
	Name string  `json:"name"`
	Data []int64 `json:"data"`
}

type compatibleLogStatsResponseBody struct {
	Intervals []time.Time                `json:"intervals"`
	Series    []compatibleLogStatsSeries `json:"series"`
}

// compatibleLogStatsResponse aligns hourly buckets with one series per level.
func compatibleLogStatsResponse(window *store.TimeWindow, stats []store.CompatibleLogStat) compatibleLogStatsResponseBody {
	start := window.Start.UTC().Truncate(time.Hour)
	end := window.End.UTC().Add(-time.Microsecond).Truncate(time.Hour)
	intervals := []time.Time{}
	index := map[time.Time]int{}
	for bucket := start; !bucket.After(end); bucket = bucket.Add(time.Hour) {
		index[bucket] = len(intervals)
		intervals = append(intervals, bucket)
	}
	series := map[string][]int64{}
	for _, item := range stats {
		position, exists := index[item.Interval.UTC().Truncate(time.Hour)]
		if !exists {
			continue
		}
		values, exists := series[item.Level]
		if !exists {
			values = make([]int64, len(intervals))
		}
		values[position] += item.Count
		series[item.Level] = values
	}
	ordered := make([]compatibleLogStatsSeries, 0, len(series))
	for _, level := range compatibleLogLevelOrder {
		if values, exists := series[level]; exists {
			ordered = append(ordered, compatibleLogStatsSeries{Name: level, Data: values})
			delete(series, level)
		}
	}
	remaining := make([]string, 0, len(series))
	for level := range series {
		remaining = append(remaining, level)
	}
	sort.Strings(remaining)
	for _, level := range remaining {
		ordered = append(ordered, compatibleLogStatsSeries{Name: level, Data: series[level]})
	}
	return compatibleLogStatsResponseBody{Intervals: intervals, Series: ordered}
}
