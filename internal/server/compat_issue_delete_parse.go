package server

import (
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"goodsentry/internal/store"
)

func compatibleIssueDeleteBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	first, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(first) != 0 {
		failure(w, 400, "Issue deletion requires an empty request body")
		return false
	}
	return true
}

func compatibleIssueDeleteQuery(w http.ResponseWriter, r *http.Request) (store.CompatibleIssueBulkFilter, bool) {
	filter := store.CompatibleIssueBulkFilter{Limit: 1000, Sort: "-last_seen"}
	query, ok := credentialQuery(w, r)
	if !ok {
		return filter, false
	}
	invalid := func(message string) (store.CompatibleIssueBulkFilter, bool) {
		failure(w, 400, message)
		return filter, false
	}
	for key, values := range query {
		if r.PathValue("project") != "" && key != "id" {
			return invalid("Project issue deletion accepts only id query parameters")
		}
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
	if !compatibleIssueIDs(w, query["id"]) {
		return filter, false
	}
	if query.Has("limit") {
		limit, valid := occurrenceNumber(query.Get("limit"), 100)
		if !valid {
			return invalid("limit must be a positive decimal integer up to 100")
		}
		filter.Limit = int(limit)
	}
	if len(query["id"]) == 0 {
		// Filtered deletion shares the existing search/time/status semantics.
		// Explicit IDs below deliberately bypass those value parsers.
		return compatibleIssueBulkQuery(w, r)
	}
	seen := make(map[string]bool, len(query["id"]))
	for _, id := range query["id"] {
		if !seen[id] {
			seen[id] = true
			filter.IDs = append(filter.IDs, id)
		}
	}
	return filter, true
}
