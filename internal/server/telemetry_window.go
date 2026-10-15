package server

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"goodsentry/internal/store"
)

var telemetryInstant = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:[Zz]|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

func queryTimeWindow(q url.Values) (*store.TimeWindow, error) {
	start, end := q.Get("start"), q.Get("end")
	if !q.Has("start") && !q.Has("end") {
		return nil, nil
	}
	parse := func(raw string) (time.Time, error) {
		if !telemetryInstant.MatchString(raw) {
			return time.Time{}, errors.New("Start and end must both be RFC3339 timestamps with an explicit time zone")
		}
		instant, err := time.Parse(time.RFC3339Nano, strings.ToUpper(raw))
		if err != nil {
			return time.Time{}, errors.New("Start and end must both be valid RFC3339 timestamps")
		}
		return instant.UTC().Truncate(time.Microsecond), nil
	}
	from, err := parse(start)
	if err != nil {
		return nil, err
	}
	to, err := parse(end)
	if err != nil {
		return nil, err
	}
	if !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		return nil, errors.New("Start must precede end by at most 366 days, at microsecond precision")
	}
	return &store.TimeWindow{Start: from, End: to}, nil
}
