package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Unknown identities remain NULL so count(DISTINCT ...) does not turn anonymous
// events into a synthetic user. Namespaces deliberately do not link attributes.
// Use Unicode White_Space explicitly: PostgreSQL's locale-dependent [:space:]
// excludes nonbreaking spaces. This only detects blanks; valid values stay exact.
const issueIdentityWhitespace = `U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'`

const issueUserIdentity = `CASE
 WHEN jsonb_typeof(e.data->'user'->'id')='string' AND btrim(e.data->'user'->>'id',` + issueIdentityWhitespace + `)<>'' THEN 'id:'||(e.data->'user'->>'id')
 WHEN jsonb_typeof(e.data->'user'->'id')='number' THEN 'id:'||trim_scale((e.data->'user'->>'id')::numeric)::text
 WHEN jsonb_typeof(e.data->'user'->'email')='string' AND btrim(e.data->'user'->>'email',` + issueIdentityWhitespace + `)<>'' THEN 'email:'||(e.data->'user'->>'email')
 WHEN jsonb_typeof(e.data->'user'->'username')='string' AND btrim(e.data->'user'->>'username',` + issueIdentityWhitespace + `)<>'' THEN 'username:'||(e.data->'user'->>'username')
 WHEN jsonb_typeof(e.data->'user'->'ip_address')='string' AND btrim(e.data->'user'->>'ip_address',` + issueIdentityWhitespace + `)<>'' AND e.data->'user'->>'ip_address'<>'{{auto}}' THEN 'ip:'||(e.data->'user'->>'ip_address')
 END`

const issueUnhandled = `EXISTS(SELECT 1 FROM jsonb_array_elements(CASE
 WHEN jsonb_typeof(e.data->'exception'->'values')='array' THEN e.data->'exception'->'values'
 WHEN jsonb_typeof(e.data->'exception')='array' THEN e.data->'exception'
 ELSE '[]'::jsonb END) exception_value WHERE exception_value->'mechanism'->'handled'='false'::jsonb)`

type issueOccurrenceFilter struct {
	environments []string
	start, end   *time.Time
	search       IssueSearch
}

func issueStateSearch(search IssueSearch) IssueSearch {
	result := IssueSearch{}
	for _, term := range search.Terms {
		if term.Field == "status" || term.Field == "category" {
			result.Terms = append(result.Terms, term)
		}
	}
	return result
}

func (f issueOccurrenceFilter) predicates(args *[]any) ([]string, error) {
	bind := func(value any) string { *args = append(*args, value); return fmt.Sprintf("$%d", len(*args)) }
	terms := []string{}
	if len(f.environments) > 0 {
		terms = append(terms, "e.environment=ANY("+bind(f.environments)+"::text[])")
	}
	if f.start != nil {
		terms = append(terms, "e.timestamp>="+bind(*f.start))
	}
	if f.end != nil {
		terms = append(terms, "e.timestamp<"+bind(*f.end))
	}
	search := IssueSearch{Text: f.search.Text}
	for _, term := range f.search.Terms {
		if term.Field != "status" && term.Field != "category" {
			search.Terms = append(search.Terms, term)
		}
	}
	_, searched, err := issueSearchPredicates(search, args)
	return append(terms, searched...), err
}

func validIssueChart(period string, auto bool) bool {
	return period == "" || period == "24h" || period == "14d" || (auto && period == "auto")
}

func prepareIssueMetrics(ctx context.Context, tx pgx.Tx, filter CompatibleIssueFilter, search IssueSearch) (issueOccurrenceFilter, time.Time, string, error) {
	f := issueOccurrenceFilter{environments: filter.Environments, start: filter.Start, end: filter.End, search: search}
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&now); err != nil {
		return f, time.Time{}, "", err
	}
	if filter.Period > 0 {
		start := now.Add(-filter.Period)
		f.start, f.end = &start, &now
	}
	end := now
	if f.end != nil {
		end = *f.end
	}
	chart := filter.StatsPeriod
	if chart == "auto" {
		chart = "14d"
		if f.start != nil && f.end != nil && f.end.Sub(*f.start) <= 24*time.Hour {
			chart = "24h"
		}
	}
	return f, end.UTC().Truncate(time.Second), chart, nil
}

type issueMetricResult struct {
	lifetime CompatibleIssueMetrics
	filtered *CompatibleIssueMetrics
}

// readIssueMetrics runs only inside its caller's authorization snapshot. SQL
// returns one aggregate and at most 24 buckets per requested current root.
func readIssueMetrics(ctx context.Context, tx pgx.Tx, ids []string, filter issueOccurrenceFilter, end time.Time, chart string) (map[string]*issueMetricResult, error) {
	result := make(map[string]*issueMetricResult, len(ids))
	args := []any{issueNumericIDs(ids)}
	terms, err := filter.predicates(&args)
	if err != nil {
		return nil, err
	}
	predicate := "true"
	if len(terms) > 0 {
		predicate = strings.Join(terms, " AND ")
	}
	for _, id := range ids {
		item := &issueMetricResult{lifetime: CompatibleIssueMetrics{Stats: emptyIssueSeries(end, chart)}}
		if len(terms) > 0 {
			item.filtered = &CompatibleIssueMetrics{Stats: emptyIssueSeries(end, chart)}
		}
		result[id] = item
	}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := tx.Query(ctx, `SELECT e.issue_id::text,count(*),count(DISTINCT `+issueUserIdentity+`),min(e.timestamp),max(e.timestamp),bool_or(`+issueUnhandled+`),
 count(*) FILTER(WHERE `+predicate+`),count(DISTINCT `+issueUserIdentity+`) FILTER(WHERE `+predicate+`),
 min(e.timestamp) FILTER(WHERE `+predicate+`),max(e.timestamp) FILTER(WHERE `+predicate+`),COALESCE(bool_or(`+issueUnhandled+`) FILTER(WHERE `+predicate+`),false)
 FROM events e WHERE e.issue_id=ANY($1::bigint[]) GROUP BY e.issue_id`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var all, matching CompatibleIssueMetrics
		if err = rows.Scan(&id, &all.Count, &all.UserCount, &all.FirstSeen, &all.LastSeen, &all.IsUnhandled, &matching.Count, &matching.UserCount, &matching.FirstSeen, &matching.LastSeen, &matching.IsUnhandled); err != nil {
			rows.Close()
			return nil, err
		}
		item := result[id]
		all.Stats = item.lifetime.Stats
		item.lifetime = all
		if item.filtered != nil {
			matching.Stats = item.filtered.Stats
			item.filtered = &matching
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(terms) > 0 {
		rows, err = tx.Query(ctx, `SELECT DISTINCT ON(e.issue_id) e.issue_id::text,e.event_id,e.environment FROM events e WHERE e.issue_id=ANY($1::bigint[]) AND `+predicate+` ORDER BY e.issue_id,e.timestamp DESC,e.id DESC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, eventID, environment string
			if err = rows.Scan(&id, &eventID, &environment); err != nil {
				rows.Close()
				return nil, err
			}
			item := result[id].filtered
			item.MatchingEventID = &eventID
			item.MatchingEnvironment = &environment
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if chart != "" {
		width, buckets := int64(3600), 24
		if chart == "14d" {
			width, buckets = 86400, 14
		}
		start := end.Add(-time.Duration(width*int64(buckets)) * time.Second)
		args = append(args, start, end, width)
		a, b, c := len(args)-2, len(args)-1, len(args)
		rows, err = tx.Query(ctx, fmt.Sprintf(`SELECT e.issue_id::text,floor(extract(epoch FROM (e.timestamp-$%d::timestamptz))/$%d::bigint)::int,count(*) FROM events e
 WHERE e.issue_id=ANY($1::bigint[]) AND (%s) AND e.timestamp>=$%d AND e.timestamp<$%d GROUP BY e.issue_id,2`, a, c, predicate, a, b), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var bucket int
			var count int64
			if err = rows.Scan(&id, &bucket, &count); err != nil {
				rows.Close()
				return nil, err
			}
			if bucket >= 0 && bucket < buckets {
				item := result[id]
				item.lifetime.Stats[chart][bucket][1] = count
				if item.filtered != nil {
					item.filtered.Stats[chart][bucket][1] = count
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	for _, item := range result {
		for _, metrics := range []*CompatibleIssueMetrics{&item.lifetime, item.filtered} {
			if metrics == nil {
				continue
			}
			metrics.FirstSeen = releaseTimeUTC(metrics.FirstSeen)
			metrics.LastSeen = releaseTimeUTC(metrics.LastSeen)
		}
	}
	return result, nil
}

func emptyIssueSeries(end time.Time, chart string) map[string][][2]int64 {
	stats := map[string][][2]int64{}
	if chart == "" {
		return stats
	}
	width, buckets := int64(3600), 24
	if chart == "14d" {
		width, buckets = 86400, 14
	}
	series := make([][2]int64, buckets)
	start := end.Unix() - width*int64(buckets)
	for i := range series {
		series[i][0] = start + int64(i)*width
	}
	stats[chart] = series
	return stats
}

func issueNumericIDs(ids []string) []int64 {
	result := make([]int64, len(ids))
	for i, id := range ids {
		result[i] = referenceID(id)
	}
	return result
}
