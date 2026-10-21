package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleStatsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/0/organizations/{org}/stats_v2/{$}", s.compatibleAny([]string{"org:read", "event:read", "event:write"}, s.compatibleStatsV2))
}

func (s *Server) compatibleStatsV2(w http.ResponseWriter, r *http.Request) {
	query, valid := performanceQuery(w, r, "category", "field", "interval", "start", "end", "outcome", "reason", "groupBy")
	if !valid {
		return
	}
	for _, required := range []string{"category", "field", "start", "end"} {
		if !query.Has(required) || query.Get(required) == "" {
			failure(w, 400, "Category, field, start and end are required")
			return
		}
	}
	category := query.Get("category")
	if category != "error" && category != "transaction" {
		failure(w, 400, "Category must be error or transaction")
		return
	}
	field := query.Get("field")
	if field != "sum(quantity)" && field != "sum(times_seen)" {
		failure(w, 400, "Field must be sum(quantity) or sum(times_seen)")
		return
	}
	interval := query.Get("interval")
	if interval == "" {
		interval = "1h"
	}
	if interval != "1m" && interval != "1h" && interval != "1d" {
		failure(w, 400, "Interval must be 1m, 1h or 1d")
		return
	}
	for _, unsupported := range []string{"outcome", "reason"} {
		if values := query[unsupported]; len(values) > 0 {
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					failure(w, 400, "Outcome and reason dimensions are not retained")
					return
				}
			}
		}
	}
	if values := query["groupBy"]; len(values) > 0 {
		for _, value := range values {
			if value != "" && value != "category" {
				failure(w, 400, "Only category grouping is supported")
				return
			}
		}
	}
	parseTime := func(value string) (time.Time, error) {
		value = strings.ToUpper(value)
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed, nil
		}
		return time.Parse("2006-01-02T15:04:05", value)
	}
	start, err := parseTime(query.Get("start"))
	if err != nil {
		failure(w, 400, "Start must be an RFC3339 timestamp")
		return
	}
	end, err := parseTime(query.Get("end"))
	if err != nil {
		failure(w, 400, "End must be an RFC3339 timestamp")
		return
	}
	projects, valid := statsV2Projects(w, query)
	if !valid {
		return
	}
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	access.ReadOnly = true
	result, err := s.db.StatsV2(r.Context(), access, category, projects, start, end, interval)
	if err != nil {
		if errors.Is(err, store.ErrInvalidStatsRange) {
			failure(w, 400, "Invalid interval range")
		} else {
			report(w, err)
		}
		return
	}
	writeJSON(w, 200, result)
}

func statsV2Projects(w http.ResponseWriter, query map[string][]string) ([]int64, bool) {
	values := query["project"]
	projects := make([]int64, 0, len(values))
	for _, value := range values {
		id, valid := occurrenceNumber(value, 9223372036854775807)
		if value != "-1" && !valid {
			failure(w, 400, "Project filters must be -1 or positive decimal int64 IDs")
			return nil, false
		}
		if value == "-1" {
			id = -1
		}
		projects = append(projects, id)
	}
	return projects, true
}
