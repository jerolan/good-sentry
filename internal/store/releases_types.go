package store

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ReleaseVersion preserves the exact identity supplied by a client.
type ReleaseVersion string

var ErrInvalidReleaseVersion = errors.New("release version must contain 1 to 255 characters without controls and cannot be . or ..")
var ErrInvalidReleaseInput = errors.New("invalid release input")

func ParseReleaseVersion(value string) (ReleaseVersion, error) {
	if !utf8.ValidString(value) || value == "" || value == "." || value == ".." || utf8.RuneCountInString(value) > 255 || strings.ContainsFunc(value, unicode.IsControl) {
		return "", ErrInvalidReleaseVersion
	}
	return ReleaseVersion(value), nil
}

// Set distinguishes an omitted update field from an explicit null.
type ReleaseString struct {
	Set   bool
	Value *string
}

type ReleaseTime struct {
	Set   bool
	Value *time.Time
}

type ReleaseUpdate struct {
	ProjectAssertions []string
	Ref               ReleaseString
	URL               ReleaseString
	DateStarted       ReleaseTime
	DateReleased      ReleaseTime
	Status            *string
	Commits           *[]ReleaseCommitInput
}

type ReleaseCreate struct {
	Version  ReleaseVersion
	Projects []string
	ReleaseUpdate
}

type Release struct {
	ID           string              `json:"id"`
	Version      ReleaseVersion      `json:"version"`
	ShortVersion ReleaseVersion      `json:"shortVersion"`
	Ref          *string             `json:"ref"`
	URL          *string             `json:"url"`
	Status       string              `json:"status"`
	DateCreated  time.Time           `json:"dateCreated"`
	DateStarted  *time.Time          `json:"dateStarted"`
	DateReleased *time.Time          `json:"dateReleased"`
	Projects     []ManagementProject `json:"projects"`
	CommitCount  int64               `json:"commitCount"`
	DeployCount  int64               `json:"deployCount"`
	LastCommit   *ReleaseCommit      `json:"lastCommit"`
	LastDeploy   *ReleaseDeploy      `json:"lastDeploy"`
	CanWrite     bool                `json:"canWrite"`
}

type ReleaseFilter struct {
	ProjectRefs  []string
	Environments []string
	Query        string
	Offset       int
	Limit        int
}

type ReleasePage struct {
	Data        []Release
	HasMore     bool
	HasPrevious bool
}

type CommitPatch struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type ReleaseCommitInput struct {
	ID          string
	Repository  string
	Message     string
	AuthorName  string
	AuthorEmail string
	Timestamp   *time.Time
	PatchSet    []CommitPatch
	Data        json.RawMessage
}

type ReleaseCommit struct {
	ID          string          `json:"id"`
	Repository  string          `json:"repository"`
	Message     string          `json:"message"`
	AuthorName  string          `json:"authorName"`
	AuthorEmail string          `json:"authorEmail"`
	DateCreated time.Time       `json:"dateCreated"`
	PatchSet    []CommitPatch   `json:"patchSet"`
	Data        json.RawMessage `json:"data"`
}

type ReleaseCommitsPage struct {
	Data        []ReleaseCommit
	HasMore     bool
	HasPrevious bool
}

type ReleaseDeployInput struct {
	Environment  string
	Name         *string
	URL          *string
	DateStarted  *time.Time
	DateFinished *time.Time
	Projects     []string
}

type ReleaseDeploy struct {
	ID           int64               `json:"id"`
	Environment  string              `json:"environment"`
	Name         *string             `json:"name"`
	URL          *string             `json:"url"`
	DateCreated  time.Time           `json:"dateCreated"`
	DateStarted  *time.Time          `json:"dateStarted"`
	DateFinished time.Time           `json:"dateFinished"`
	Projects     []ManagementProject `json:"projects"`
}

type ReleaseDeploysPage struct {
	Data        []ReleaseDeploy
	HasMore     bool
	HasPrevious bool
}

type ReleaseRepository struct {
	ID          string                    `json:"id"`
	Name        string                    `json:"name"`
	DateCreated time.Time                 `json:"dateCreated"`
	URL         *string                   `json:"url"`
	Status      string                    `json:"status"`
	Provider    ReleaseRepositoryProvider `json:"provider"`
}

type ReleaseRepositoryProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ReleaseRepositoriesPage struct {
	Data        []ReleaseRepository
	HasMore     bool
	HasPrevious bool
}
