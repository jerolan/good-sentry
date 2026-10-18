package server

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodsentry/internal/eventview"
	"goodsentry/internal/store"
)

type compatibleIssueProject struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type compatibleIssueStatusDetails struct {
	IgnoreUntil   *time.Time `json:"ignoreUntil,omitempty"`
	IgnoreCount   *int32     `json:"ignoreCount,omitempty"`
	InNextRelease bool       `json:"inNextRelease,omitempty"`
	InRelease     string     `json:"inRelease,omitempty"`
}

type compatibleIssueLifetime struct {
	Count     string    `json:"count"`
	UserCount int64     `json:"userCount"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

type compatibleIssueFiltered struct {
	Count       string                `json:"count"`
	UserCount   int64                 `json:"userCount"`
	FirstSeen   *time.Time            `json:"firstSeen"`
	LastSeen    *time.Time            `json:"lastSeen"`
	IsUnhandled bool                  `json:"isUnhandled"`
	Stats       map[string][][2]int64 `json:"stats"`
}

type compatibleIssueRelease struct {
	Version      string     `json:"version"`
	ShortVersion string     `json:"shortVersion"`
	DateCreated  time.Time  `json:"dateCreated"`
	DateReleased *time.Time `json:"dateReleased"`
}

type compatibleIssueView struct {
	ID                       string                       `json:"id"`
	Count                    string                       `json:"count"`
	ShortID                  string                       `json:"shortId"`
	Title                    string                       `json:"title"`
	Level                    string                       `json:"level"`
	Status                   string                       `json:"status"`
	Type                     string                       `json:"type"`
	Metadata                 map[string]string            `json:"metadata"`
	Project                  compatibleIssueProject       `json:"project"`
	AssignedTo               *store.IssueActor            `json:"assignedTo"`
	NumComments              int64                        `json:"numComments"`
	UserCount                int64                        `json:"userCount"`
	FirstSeen                time.Time                    `json:"firstSeen"`
	LastSeen                 time.Time                    `json:"lastSeen"`
	Permalink                string                       `json:"permalink"`
	Logger                   *string                      `json:"logger,omitempty"`
	Culprit                  *string                      `json:"culprit,omitempty"`
	StatusDetails            compatibleIssueStatusDetails `json:"statusDetails"`
	Stats                    map[string][][2]int64        `json:"stats"`
	Lifetime                 compatibleIssueLifetime      `json:"lifetime"`
	Filtered                 *compatibleIssueFiltered     `json:"filtered,omitempty"`
	IsUnhandled              bool                         `json:"isUnhandled"`
	MatchingEventID          *string                      `json:"matchingEventId"`
	MatchingEventEnvironment *string                      `json:"matchingEventEnvironment"`
	FirstRelease             *compatibleIssueRelease      `json:"firstRelease"`
	LastRelease              *compatibleIssueRelease      `json:"lastRelease"`
	UserReportCount          *int64                       `json:"userReportCount,omitempty"`
}

type compatibleIssueStatsView struct {
	ID          string                `json:"id"`
	Count       string                `json:"count"`
	UserCount   int64                 `json:"userCount"`
	FirstSeen   time.Time             `json:"firstSeen"`
	LastSeen    time.Time             `json:"lastSeen"`
	IsUnhandled bool                  `json:"isUnhandled"`
	Stats       map[string][][2]int64 `json:"stats"`
}

func issueWireText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func issueWireTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	stamp := value.UTC()
	return &stamp
}

func issueWireStats(value map[string][][2]int64) map[string][][2]int64 {
	result := make(map[string][][2]int64, len(value))
	for key, buckets := range value {
		result[key] = append([][2]int64{}, buckets...)
	}
	return result
}

func issueWireRelease(value *eventview.ReleaseReference) *compatibleIssueRelease {
	if value == nil {
		return nil
	}
	return &compatibleIssueRelease{Version: value.Version, ShortVersion: value.Version, DateCreated: value.DateCreated.UTC(), DateReleased: issueWireTime(value.DateReleased)}
}

func projectCompatibleIssue(record store.CompatibleIssueRecord, domain string, detail bool) (compatibleIssueView, error) {
	i := record.Issue
	view := compatibleIssueView{
		ID: i.ID, Count: strconv.FormatInt(i.Count, 10), ShortID: i.ShortID,
		Title: issueWireText(i.Title, 255), Level: i.Level, Status: i.Status,
		Type: "default", Metadata: map[string]string{},
		Project:    compatibleIssueProject{ID: i.Project.ID, Slug: i.Project.Slug, Name: issueWireText(i.Project.Name, 64), Platform: issueWireText(record.ProjectPlatform, 64)},
		AssignedTo: i.AssignedTo, NumComments: record.NumComments, UserCount: record.UserCount,
		FirstSeen: i.FirstSeen.UTC(), LastSeen: i.LastSeen.UTC(), IsUnhandled: record.IsUnhandled,
		Stats: issueWireStats(record.Stats), FirstRelease: issueWireRelease(record.FirstRelease), LastRelease: issueWireRelease(record.LastRelease),
		Lifetime:  compatibleIssueLifetime{Count: strconv.FormatInt(i.Count, 10), UserCount: record.UserCount, FirstSeen: i.FirstSeen.UTC(), LastSeen: i.LastSeen.UTC()},
		Permalink: strings.TrimRight(domain, "/") + "/?" + url.Values{"org": {record.OrganizationSlug}, "issue": {i.ID}}.Encode(),
	}
	if record.Latest != nil {
		latest, err := eventview.Project(*record.Latest, false)
		if err != nil {
			return view, err
		}
		view.Type, view.Metadata, view.Logger = latest.Type, latest.Metadata, latest.Logger
		if latest.Culprit != nil {
			culprit := issueWireText(*latest.Culprit, 1024)
			view.Culprit = &culprit
		}
		id, environment := record.Latest.ID, record.Latest.Environment
		view.MatchingEventID, view.MatchingEventEnvironment = &id, &environment
	}
	if filtered := record.Filtered; filtered != nil {
		view.Filtered = &compatibleIssueFiltered{Count: strconv.FormatInt(filtered.Count, 10), UserCount: filtered.UserCount, FirstSeen: issueWireTime(filtered.FirstSeen), LastSeen: issueWireTime(filtered.LastSeen), IsUnhandled: filtered.IsUnhandled, Stats: issueWireStats(filtered.Stats)}
		view.MatchingEventID, view.MatchingEventEnvironment = filtered.MatchingEventID, filtered.MatchingEnvironment
	}
	if ignored := i.Ignore; ignored != nil {
		view.StatusDetails.IgnoreUntil, view.StatusDetails.IgnoreCount = issueWireTime(ignored.Until), ignored.Count
	}
	if resolution := i.Resolution; resolution != nil {
		if resolution.Release != nil {
			view.StatusDetails.InRelease = string(resolution.Release.Version)
		} else if resolution.Mode == "next_release" {
			view.StatusDetails.InNextRelease = true
		}
	}
	if detail {
		count := record.UserReportCount
		view.UserReportCount = &count
	}
	return view, nil
}
