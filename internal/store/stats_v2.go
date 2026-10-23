package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidStatsRange = errors.New("invalid stats range")

type StatsV2By struct {
	Reason   string `json:"reason"`
	Category string `json:"category"`
	Outcome  string `json:"outcome"`
}

type StatsV2Totals struct {
	SumTimesSeen int64 `json:"sum(times_seen)"`
	SumQuantity  int64 `json:"sum(quantity)"`
}

type StatsV2Series struct {
	SumTimesSeen []int64 `json:"sum(times_seen)"`
	SumQuantity  []int64 `json:"sum(quantity)"`
}

type StatsV2Group struct {
	By     StatsV2By     `json:"by"`
	Totals StatsV2Totals `json:"totals"`
	Series StatsV2Series `json:"series"`
}

type StatsV2Response struct {
	Start     time.Time      `json:"start"`
	End       time.Time      `json:"end"`
	Intervals []time.Time    `json:"intervals"`
	Groups    []StatsV2Group `json:"groups"`
}

func statsV2Interval(value string) (time.Duration, string, bool) {
	switch value {
	case "1m":
		return time.Minute, "minute", true
	case "", "1h":
		return time.Hour, "hour", true
	case "1d":
		return 24 * time.Hour, "day", true
	default:
		return 0, "", false
	}
}

func statsV2Buckets(start, end time.Time, interval time.Duration) ([]time.Time, bool) {
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		return nil, false
	}
	alignedStart := start.UTC().Truncate(interval)
	last := end.UTC().Add(-time.Microsecond).Truncate(interval)
	if last.Before(alignedStart) {
		return nil, false
	}
	count := int(last.Sub(alignedStart)/interval) + 1
	if count < 1 || count > 1000 {
		return nil, false
	}
	buckets := make([]time.Time, count)
	for index := range buckets {
		buckets[index] = alignedStart.Add(time.Duration(index) * interval)
	}
	return buckets, true
}

func (s *Store) StatsV2(ctx context.Context, access ManagementAccess, category string, projectIDs []int64, start, end time.Time, interval string) (StatsV2Response, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || category != "error" && category != "transaction" {
		return StatsV2Response{}, ErrInvalidStatsRange
	}
	duration, unit, valid := statsV2Interval(interval)
	if !valid {
		return StatsV2Response{}, ErrInvalidStatsRange
	}
	buckets, valid := statsV2Buckets(start, end, duration)
	if !valid {
		return StatsV2Response{}, ErrInvalidStatsRange
	}
	allProjects := len(projectIDs) == 0
	selected := make([]int64, 0, len(projectIDs))
	for _, id := range projectIDs {
		if id == -1 {
			allProjects = true
			continue
		}
		if id <= 0 {
			return StatsV2Response{}, ErrInvalidStatsRange
		}
		selected = append(selected, id)
	}
	if allProjects {
		selected = nil
	} else {
		projects, err := scopedProjectIDs(ctx, s, access, selected)
		if err != nil {
			return StatsV2Response{}, err
		}
		selected = projects
	}
	return withSpanArchiveRead(ctx, s, func(read *spanArchiveRead) (StatsV2Response, error) {
		if category == "transaction" {
			isSegment := true
			selector := spanArchiveSelector{TimeColumn: "end_timestamp", Start: &start, End: &end, IsSegment: &isSegment}
			if len(selected) > 0 {
				selector.RestrictProjectIDs, selector.ProjectIDs = true, selected
			}
			if err := read.loadAccessSpans(ctx, access, selector); err != nil {
				return StatsV2Response{}, err
			}
		}
		args := []any{access.OrganizationID, access.UserID, access.AllProjects, access.ProjectIDs, unit, start.UTC(), end.UTC()}
		projectFilter := ""
		if len(selected) > 0 {
			args = append(args, selected)
			projectFilter = fmt.Sprintf(" AND p.id=ANY($%d::bigint[])", len(args))
		}
		kind := "e.kind IN ('error','message')"
		if category == "transaction" {
			kind = "e.kind='transaction'"
		}
		query := `WITH telemetry AS (
 SELECT date_trunc($5::text,e.timestamp) AS bucket
 FROM events e JOIN projects p ON p.id=e.project_id
 JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[]))` + projectFilter + `
 AND ` + kind + ` AND e.timestamp>=$6::timestamptz AND e.timestamp<$7::timestamptz
`
		if category == "transaction" {
			query += ` UNION ALL
 SELECT date_trunc($5::text,s.end_timestamp) AS bucket
 FROM spans s JOIN projects p ON p.id=s.project_id
 JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id::text=$2
 WHERE p.organization_id::text=$1 AND ($3 OR p.id=ANY($4::bigint[]))` + projectFilter + `
 AND s.is_segment AND s.end_timestamp>=$6::timestamptz AND s.end_timestamp<$7::timestamptz
`
		}
		query += `) SELECT bucket,count(*) FROM telemetry GROUP BY bucket ORDER BY bucket`
		rows, err := read.tx.Query(ctx, query, args...)
		if err != nil {
			return StatsV2Response{}, err
		}
		defer rows.Close()
		series := make([]int64, len(buckets))
		index := map[time.Time]int{}
		for position, bucket := range buckets {
			index[bucket] = position
		}
		var total int64
		for rows.Next() {
			var bucket time.Time
			var count int64
			if err = rows.Scan(&bucket, &count); err != nil {
				return StatsV2Response{}, err
			}
			position, exists := index[bucket.UTC()]
			if !exists {
				continue
			}
			series[position] = count
			total += count
		}
		if err = rows.Err(); err != nil {
			return StatsV2Response{}, err
		}
		return StatsV2Response{
			Start:     buckets[0].UTC(),
			End:       buckets[len(buckets)-1].UTC().Add(duration),
			Intervals: buckets,
			Groups: []StatsV2Group{{
				By:     StatsV2By{Category: category},
				Totals: StatsV2Totals{SumTimesSeen: total, SumQuantity: total},
				Series: StatsV2Series{SumTimesSeen: series, SumQuantity: append([]int64(nil), series...)},
			}},
		}, nil
	})
}
