package server

import (
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/store"
)

type compatibleEventPaging struct {
	query  url.Values
	filter store.CompatibleEventFilter
	full   bool
}

func compatibleEventQuery(w http.ResponseWriter, r *http.Request, list bool) (url.Values, []string, bool) {
	q, ok := credentialQuery(w, r)
	if !ok {
		return nil, nil, false
	}
	for key, values := range q {
		if key == "environment" {
			if len(values) > 100 {
				failure(w, 400, "At most 100 environment values are allowed")
				return nil, nil, false
			}
			for _, value := range values {
				if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 200 || strings.ContainsRune(value, 0) {
					failure(w, 400, "Environments must contain at most 200 characters without NUL")
					return nil, nil, false
				}
			}
			continue
		}
		known := false
		if list {
			switch key {
			case "limit", "per_page", "cursor", "full", "sample", "query", "start", "end", "statsPeriod":
				known = true
			}
		}
		if !known || len(values) != 1 {
			failure(w, 400, "Unknown or repeated event query parameter")
			return nil, nil, false
		}
	}
	return q, q["environment"], true
}

func compatibleEventListQuery(w http.ResponseWriter, r *http.Request) (compatibleEventPaging, bool) {
	q, environments, ok := compatibleEventQuery(w, r, true)
	if !ok {
		return compatibleEventPaging{}, false
	}
	paging := compatibleEventPaging{query: q, filter: store.CompatibleEventFilter{Environments: environments, Query: q.Get("query"), Limit: 100}}
	for _, key := range []string{"limit", "per_page"} {
		if q.Has(key) {
			value, valid := occurrenceNumber(q.Get(key), 100)
			if !valid {
				failure(w, 400, key+" must be a positive decimal integer up to 100")
				return paging, false
			}
			paging.filter.Limit = int(value)
		}
	}
	if q.Has("limit") && q.Has("per_page") && q.Get("limit") != q.Get("per_page") {
		failure(w, 400, "limit and per_page must agree")
		return paging, false
	}
	for _, setting := range []struct {
		key   string
		value *bool
	}{{"full", &paging.full}, {"sample", &paging.filter.Sample}} {
		if !q.Has(setting.key) {
			continue
		}
		switch q.Get(setting.key) {
		case "true", "1":
			*setting.value = true
		case "false", "0":
			*setting.value = false
		default:
			failure(w, 400, setting.key+" must be true, false, 1 or 0")
			return paging, false
		}
	}
	if paging.full {
		paging.filter.Limit = min(10, paging.filter.Limit)
	}
	if value := q.Get("cursor"); value != "" {
		parts := strings.Split(value, ":")
		if len(parts) != 3 || parts[0] != "0" || (parts[2] != "0" && parts[2] != "1") {
			failure(w, 400, "Cursor is invalid")
			return paging, false
		}
		offset, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || offset < 0 || offset > maxProjectOffset || strconv.FormatInt(offset, 10) != parts[1] {
			failure(w, 400, "Cursor is invalid")
			return paging, false
		}
		paging.filter.Offset = int(offset)
	}
	for _, field := range []struct {
		key   string
		value **time.Time
	}{{"start", &paging.filter.Start}, {"end", &paging.filter.End}} {
		if q.Has(field.key) {
			value, valid := compatibleEventTime(q.Get(field.key))
			if !valid {
				failure(w, 400, field.key+" must be a valid ISO datetime")
				return paging, false
			}
			*field.value = &value
		}
	}
	if paging.filter.Start != nil && paging.filter.End != nil && !paging.filter.End.After(*paging.filter.Start) {
		failure(w, 400, "end must be after start")
		return paging, false
	}
	if value := q.Get("statsPeriod"); value != "" {
		var valid bool
		paging.filter.Period, valid = compatibleEventPeriod(value)
		if !valid {
			failure(w, 400, "statsPeriod must be a positive integer followed by s, m, h, d or w")
			return paging, false
		}
	}
	return paging, true
}

var compatibleEventTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])?$`)

func compatibleEventTime(value string) (time.Time, bool) {
	if !compatibleEventTimePattern.MatchString(value) {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		parsed, err = time.Parse("2006-01-02T15:04:05.999999999", value)
	}
	return parsed.UTC(), err == nil
}

func compatibleEventPeriod(value string) (time.Duration, bool) {
	if len(value) < 2 {
		return 0, false
	}
	var unit time.Duration
	switch value[len(value)-1] {
	case 's':
		unit = time.Second
	case 'm':
		unit = time.Minute
	case 'h':
		unit = time.Hour
	case 'd':
		unit = 24 * time.Hour
	case 'w':
		unit = 7 * 24 * time.Hour
	default:
		return 0, false
	}
	number := value[:len(value)-1]
	for _, c := range number {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt64/int64(unit) {
		return 0, false
	}
	return time.Duration(n) * unit, true
}
