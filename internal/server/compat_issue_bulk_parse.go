package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/store"
)

func parseCompatibleIssueBulkChange(fields map[string]json.RawMessage) (store.CompatibleIssueBulkChange, error) {
	var result store.CompatibleIssueBulkChange
	individual := make(map[string]json.RawMessage, len(fields))
	for key, raw := range fields {
		if key != "merge" && key != "snoozeDuration" {
			individual[key] = raw
		}
	}
	if raw, present := fields["snoozeDuration"]; present && !compatibleIssueNull(raw) {
		value, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 32)
		if err != nil || value < 0 {
			return result, errors.New("snoozeDuration must be null or a JSON integer between 0 and 2147483647")
		}
		if duration, present := individual["ignoreDuration"]; present {
			other, err := strconv.ParseInt(string(bytes.TrimSpace(duration)), 10, 32)
			if err != nil || value != other {
				return result, errors.New("snoozeDuration and ignoreDuration must agree")
			}
		}
		// The individual parser also compares this duration with a nested copy,
		// retaining explicit zero until that comparison has completed.
		individual["ignoreDuration"] = raw
	}
	if raw, present := fields["merge"]; present {
		switch string(bytes.TrimSpace(raw)) {
		case "true", "1":
			result.Merge = true
		case "false", "0", "null":
		default:
			return result, errors.New("merge must be true, false, 1, 0 or null")
		}
	}
	var err error
	result.Change, err = parseCompatibleIssueChange(individual)
	return result, err
}

func compatibleIssueBulkQuery(w http.ResponseWriter, r *http.Request) (store.CompatibleIssueBulkFilter, bool) {
	f := store.CompatibleIssueBulkFilter{Limit: 1000, Sort: "-last_seen"}
	q, ok := credentialQuery(w, r)
	if !ok {
		return f, false
	}
	invalid := func(message string) (store.CompatibleIssueBulkFilter, bool) {
		failure(w, 400, message)
		return f, false
	}
	for key, values := range q {
		switch key {
		case "id":
			if len(values) > 1000 {
				return invalid("id allows at most 1000 values")
			}
		case "project", "environment":
			if len(values) > 100 {
				return invalid(key + " allows at most 100 values")
			}
		case "query", "status", "sort", "limit", "start", "end", "statsPeriod":
			if len(values) != 1 {
				return invalid("Scalar issue query parameters may not be repeated")
			}
		default:
			return invalid("Unknown issue query parameter")
		}
		for _, value := range values {
			if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
				return invalid("Issue query parameters must be valid UTF-8 without NUL")
			}
		}
	}
	if !compatibleIssueIDs(w, q["id"]) || !compatibleIssueEnvironments(w, q["environment"]) {
		return f, false
	}
	for _, id := range q["id"] {
		if !slices.Contains(f.IDs, id) {
			f.IDs = append(f.IDs, id)
		}
	}
	f.ProjectRefs, f.Environments = q["project"], q["environment"]
	for _, ref := range f.ProjectRefs {
		if ref == "" || ref == "-1" && len(f.ProjectRefs) != 1 {
			return invalid("Project selectors must be exact IDs or slugs; -1 must be used alone")
		}
	}
	if q.Has("query") {
		f.Query = q.Get("query")
	} else if r.PathValue("project") == "" && len(f.IDs) == 0 {
		f.Query = "is:unresolved"
	}
	if q.Has("status") {
		f.Status = q.Get("status")
		switch f.Status {
		case "unresolved", "resolved", "ignored":
		case "muted":
			f.Status = "ignored"
		default:
			return invalid("Issue status filter must be unresolved, resolved, ignored or muted")
		}
	}
	if q.Has("sort") {
		f.Sort = q.Get("sort")
	}
	switch f.Sort {
	case "last_seen", "-last_seen", "first_seen", "-first_seen", "count", "-count", "priority", "-priority", "date", "new", "freq", "user":
	default:
		return invalid("Issue sort is not implemented")
	}
	if q.Has("limit") {
		limit, valid := occurrenceNumber(q.Get("limit"), 100)
		if !valid {
			return invalid("limit must be a positive decimal integer up to 100")
		}
		f.Limit = int(limit)
	}
	for _, bound := range []struct {
		key   string
		value **time.Time
	}{{"start", &f.Start}, {"end", &f.End}} {
		if q.Has(bound.key) {
			stamp, valid := compatibleEventTime(q.Get(bound.key))
			if !valid {
				return invalid(bound.key + " must be a valid ISO datetime")
			}
			*bound.value = &stamp
		}
	}
	if f.Start != nil && f.End != nil && !f.End.After(*f.Start) {
		return invalid("end must be after start")
	}
	if period := q.Get("statsPeriod"); period != "" {
		var valid bool
		f.Period, valid = compatibleEventPeriod(period)
		if !valid {
			return invalid("statsPeriod must be a positive integer followed by s, m, h, d or w")
		}
	}
	return f, true
}
