package server

import (
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/store"
)

func compatibleIssueQuery(w http.ResponseWriter, r *http.Request, scalars, repeated []string) (url.Values, bool) {
	q, ok := credentialQuery(w, r)
	if !ok {
		return nil, false
	}
	for key, values := range q {
		if slices.Contains(repeated, key) {
			if len(values) > 100 {
				failure(w, 400, key+" allows at most 100 values")
				return nil, false
			}
		} else if !slices.Contains(scalars, key) || len(values) != 1 {
			failure(w, 400, "Unknown or repeated issue query parameter")
			return nil, false
		}
	}
	return q, true
}

func compatibleIssueEnvironments(w http.ResponseWriter, values []string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || utf8.RuneCountInString(value) > 200 {
			failure(w, 400, "Environments must contain at most 200 characters without NUL")
			return false
		}
	}
	return true
}

func compatibleIssueIDs(w http.ResponseWriter, values []string) bool {
	for _, value := range values {
		if _, ok := occurrenceNumber(value, math.MaxInt64); !ok {
			failure(w, 400, "Issue IDs must be canonical positive decimal int64 strings")
			return false
		}
	}
	return true
}

func compatibleShortIDReference(value string) (string, bool) {
	separator := strings.LastIndexByte(value, '-')
	if separator < 1 || separator == len(value)-1 || !utf8.ValidString(value) {
		return "", false
	}
	project := strings.ToLower(value[:separator])
	if len(project) > 64 || store.Slug(project) != project {
		return "", false
	}
	if _, valid := occurrenceNumber(value[separator+1:], math.MaxInt64); !valid {
		return "", false
	}
	return strings.ToUpper(project) + value[separator:], true
}

func compatibleIssueListQuery(w http.ResponseWriter, r *http.Request) (store.CompatibleIssueFilter, url.Values, bool) {
	f := store.CompatibleIssueFilter{Limit: 100, Query: "is:unresolved", Sort: "-last_seen", StatsPeriod: "24h"}
	q, ok := compatibleIssueQuery(w, r, []string{"query", "sort", "shortIdLookup", "limit", "per_page", "cursor", "start", "end", "statsPeriod", "groupStatsPeriod"}, []string{"id", "project", "environment"})
	if !ok {
		return f, nil, false
	}
	invalid := func(message string) (store.CompatibleIssueFilter, url.Values, bool) {
		failure(w, 400, message)
		return f, q, false
	}
	f.IDs, f.ProjectRefs, f.Environments = q["id"], q["project"], q["environment"]
	if !compatibleIssueIDs(w, f.IDs) || !compatibleIssueEnvironments(w, f.Environments) {
		return f, q, false
	}
	for _, ref := range f.ProjectRefs {
		if ref == "" || !utf8.ValidString(ref) || strings.ContainsRune(ref, 0) || ref == "-1" && len(f.ProjectRefs) != 1 {
			return invalid("Project selectors must be exact IDs or slugs; -1 must be used alone")
		}
	}
	if q.Has("query") {
		f.Query = q.Get("query")
	}
	if q.Has("sort") {
		f.Sort = q.Get("sort")
	}
	switch f.Sort {
	case "last_seen", "-last_seen", "first_seen", "-first_seen", "count", "-count", "priority", "-priority", "date", "new", "freq", "user":
	default:
		return invalid("Issue sort is not implemented")
	}
	if q.Has("shortIdLookup") {
		switch q.Get("shortIdLookup") {
		case "1", "true":
			f.ShortIDLookup = true
		case "0", "false":
		default:
			return invalid("shortIdLookup must be 0, 1, false or true")
		}
	}
	for _, key := range []string{"limit", "per_page"} {
		if q.Has(key) {
			limit, valid := occurrenceNumber(q.Get(key), 100)
			if !valid {
				return invalid(key + " must be a positive decimal integer up to 100")
			}
			f.Limit = int(limit)
		}
	}
	if q.Has("limit") && q.Has("per_page") && q.Get("limit") != q.Get("per_page") {
		return invalid("limit and per_page must agree")
	}
	if cursor := q.Get("cursor"); cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 3 || parts[0] != "0" || parts[2] != "0" && parts[2] != "1" {
			return invalid("Cursor is invalid")
		}
		offset, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || offset < 0 || offset > maxProjectOffset || strconv.FormatInt(offset, 10) != parts[1] {
			return invalid("Cursor is invalid")
		}
		f.Offset = int(offset)
	}
	for _, field := range []struct {
		key   string
		value **time.Time
	}{{"start", &f.Start}, {"end", &f.End}} {
		if q.Has(field.key) {
			stamp, valid := compatibleEventTime(q.Get(field.key))
			if !valid {
				return invalid(field.key + " must be a valid ISO datetime")
			}
			*field.value = &stamp
		}
	}
	if f.Start != nil && f.End != nil && !f.End.After(*f.Start) {
		return invalid("end must be after start")
	}
	if q.Has("statsPeriod") {
		period := q.Get("statsPeriod")
		if r.PathValue("project") != "" {
			if period != "" && period != "24h" && period != "14d" {
				return invalid("Project statsPeriod must be empty, 24h or 14d")
			}
			f.StatsPeriod = period
		} else if period != "" {
			var valid bool
			f.Period, valid = compatibleEventPeriod(period)
			if !valid {
				return invalid("Organization statsPeriod must be a positive integer followed by s, m, h, d or w")
			}
		}
	}
	if q.Has("groupStatsPeriod") {
		chart := q.Get("groupStatsPeriod")
		if chart != "" && chart != "24h" && chart != "14d" && chart != "auto" {
			return invalid("groupStatsPeriod must be empty, 24h, 14d or auto")
		}
		f.StatsPeriod = chart
	}
	return f, q, true
}

func compatibleIssueDetailQuery(w http.ResponseWriter, r *http.Request) (store.CompatibleIssueDetailFilter, bool) {
	f := store.CompatibleIssueDetailFilter{StatsPeriod: "24h"}
	q, ok := compatibleIssueQuery(w, r, []string{"statsPeriod"}, []string{"environment"})
	if !ok {
		return f, false
	}
	f.Environments = q["environment"]
	if !compatibleIssueEnvironments(w, f.Environments) {
		return f, false
	}
	if q.Has("statsPeriod") {
		f.StatsPeriod = q.Get("statsPeriod")
	}
	if f.StatsPeriod != "" && f.StatsPeriod != "24h" && f.StatsPeriod != "14d" {
		failure(w, 400, "Detail statsPeriod must be empty, 24h or 14d")
		return f, false
	}
	return f, true
}

func compatibleIssueStatsQuery(w http.ResponseWriter, r *http.Request) ([]string, string, bool) {
	q, ok := compatibleIssueQuery(w, r, []string{"statsPeriod"}, []string{"groups"})
	if !ok {
		return nil, "", false
	}
	ids := q["groups"]
	if len(ids) == 0 {
		failure(w, 400, "Provide 1 to 100 group IDs")
		return nil, "", false
	}
	if !compatibleIssueIDs(w, ids) {
		return nil, "", false
	}
	period := "24h"
	if q.Has("statsPeriod") {
		period = q.Get("statsPeriod")
	}
	if period != "24h" && period != "14d" {
		failure(w, 400, "Issue statsPeriod must be 24h or 14d")
		return nil, "", false
	}
	return ids, period, true
}
