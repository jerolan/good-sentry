package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/store"
)

var releaseInstantPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt][0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?([Zz]|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func releaseBody(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	var raw json.RawMessage
	if !bodyJSON(w, r, &raw) {
		return nil, false
	}
	if !utf8.Valid(raw) {
		failure(w, 400, "Release metadata requires UTF-8 JSON")
		return nil, false
	}
	return raw, true
}

func releaseObject(raw json.RawMessage, allowed ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, errors.New("Expected a release metadata object")
	}
	for field := range fields {
		if !slices.Contains(allowed, field) {
			return nil, fmt.Errorf("Unsupported release field: %s", field)
		}
	}
	return fields, nil
}

func releaseText(raw json.RawMessage, name string, maximum int, nonempty bool) (string, error) {
	var value string
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || !credentialText(value, maximum) || (nonempty && value == "") {
		return "", fmt.Errorf("%s must be text of at most %d characters without controls", name, maximum)
	}
	return value, nil
}

func releaseNullableText(raw json.RawMessage, name string, maximum int) (store.ReleaseString, error) {
	if raw == nil {
		return store.ReleaseString{}, nil
	}
	result := store.ReleaseString{Set: true}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return result, nil
	}
	value, err := releaseText(raw, name, maximum, false)
	result.Value = &value
	return result, err
}

func releaseTime(raw json.RawMessage, name string) (store.ReleaseTime, error) {
	if raw == nil {
		return store.ReleaseTime{}, nil
	}
	result := store.ReleaseTime{Set: true}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return result, nil
	}
	value, err := releaseText(raw, name, 100, true)
	if err != nil || !releaseInstantPattern.MatchString(value) {
		return result, fmt.Errorf("%s must be an RFC3339 instant with an explicit time zone", name)
	}
	instant, err := time.Parse(time.RFC3339Nano, strings.ToUpper(value))
	instant = instant.UTC().Truncate(time.Microsecond)
	if err != nil || instant.Year() < 1 || instant.Year() > 9999 {
		return result, fmt.Errorf("%s must be a valid instant in years 1 to 9999", name)
	}
	result.Value = &instant
	return result, nil
}

func releaseProjectRefs(raw json.RawMessage, required bool) ([]string, error) {
	if raw == nil && !required {
		return nil, nil
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 || len(values) > 1000 {
		return nil, errors.New("Projects must contain 1 to 1000 project slugs or IDs")
	}
	projects := make([]string, 0, len(values))
	for _, raw := range values {
		value, err := releaseText(raw, "Project reference", 200, true)
		if err != nil {
			return nil, err
		}
		projects = append(projects, value)
	}
	return projects, nil
}

func decodeReleaseUpdate(fields map[string]json.RawMessage) (store.ReleaseUpdate, error) {
	var update store.ReleaseUpdate
	var err error
	if update.Ref, err = releaseNullableText(fields["ref"], "ref", 2000); err != nil {
		return update, err
	}
	if update.URL, err = releaseNullableText(fields["url"], "url", 2000); err != nil {
		return update, err
	}
	if update.DateStarted, err = releaseTime(fields["dateStarted"], "dateStarted"); err != nil {
		return update, err
	}
	if update.DateReleased, err = releaseTime(fields["dateReleased"], "dateReleased"); err != nil {
		return update, err
	}
	if raw := fields["status"]; raw != nil {
		value, err := releaseText(raw, "status", 8, true)
		if err != nil || (value != "open" && value != "archived") {
			return update, errors.New("Status must be open or archived")
		}
		update.Status = &value
	}
	if raw := fields["commits"]; raw != nil {
		commits, err := decodeReleaseCommits(raw)
		if err != nil {
			return update, err
		}
		update.Commits = &commits
	}
	return update, nil
}

func releaseAlias(fields map[string]json.RawMessage, first, second string) (json.RawMessage, error) {
	a, b := fields[first], fields[second]
	if first == second {
		return a, nil
	}
	if a == nil {
		return b, nil
	}
	if b == nil {
		return a, nil
	}
	// Keep numeric tokens exact when comparing structured aliases. Unknown
	// patch metadata can contain integers beyond float64 precision.
	var left, right any
	leftDecoder := json.NewDecoder(bytes.NewReader(a))
	rightDecoder := json.NewDecoder(bytes.NewReader(b))
	leftDecoder.UseNumber()
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&left) != nil || rightDecoder.Decode(&right) != nil {
		return nil, fmt.Errorf("Invalid aliases %s and %s", first, second)
	}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	if !bytes.Equal(leftJSON, rightJSON) {
		return nil, fmt.Errorf("Conflicting aliases %s and %s", first, second)
	}
	return a, nil
}

func decodeReleaseCommits(raw json.RawMessage) ([]store.ReleaseCommitInput, error) {
	var records []json.RawMessage
	if json.Unmarshal(raw, &records) != nil || records == nil || len(records) > 1000 {
		return nil, errors.New("Commits must be an array of at most 1000 objects; null is not allowed")
	}
	result := make([]store.ReleaseCommitInput, 0, len(records))
	seen := map[[2]string]bool{}
	for _, record := range records {
		var fields map[string]json.RawMessage
		if json.Unmarshal(record, &fields) != nil || fields == nil {
			return nil, errors.New("Each commit must be an object")
		}
		var commit store.ReleaseCommitInput
		var err error
		if commit.ID, err = releaseText(fields["id"], "Commit id", 200, true); err != nil {
			return nil, err
		}
		for _, field := range []struct {
			name, alias string
			maximum     int
			destination *string
		}{{"repository", "repository", 200, &commit.Repository}, {"message", "message", 10000, &commit.Message}, {"authorName", "author_name", 200, &commit.AuthorName}, {"authorEmail", "author_email", 320, &commit.AuthorEmail}} {
			value, err := releaseAlias(fields, field.name, field.alias)
			if err != nil {
				return nil, err
			}
			if value != nil {
				// Git messages may contain newlines, tabs and other textual
				// controls. They remain metadata and are always rendered as text.
				if field.name == "message" {
					if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, field.destination) != nil || !utf8.ValidString(*field.destination) || utf8.RuneCountInString(*field.destination) > field.maximum || strings.ContainsRune(*field.destination, 0) {
						return nil, errors.New("Commit message must be text of at most 10000 characters without NUL")
					}
				} else if *field.destination, err = releaseText(value, field.name, field.maximum, false); err != nil {
					return nil, err
				}
			}
		}
		stamp, err := releaseAlias(fields, "timestamp", "dateCreated")
		if err != nil {
			return nil, err
		}
		parsedTime, err := releaseTime(stamp, "Commit timestamp")
		if err != nil || (stamp != nil && parsedTime.Value == nil) {
			return nil, errors.New("Commit timestamp must be an RFC3339 instant, not null")
		}
		commit.Timestamp = parsedTime.Value
		patches, err := releaseAlias(fields, "patchSet", "patch_set")
		if err != nil {
			return nil, err
		}
		commit.PatchSet = []store.CommitPatch{}
		if patches != nil {
			var entries []json.RawMessage
			if json.Unmarshal(patches, &entries) != nil || entries == nil || len(entries) > 1000 {
				return nil, errors.New("Patch set must be an array of at most 1000 objects")
			}
			for _, entry := range entries {
				var patch map[string]json.RawMessage
				if json.Unmarshal(entry, &patch) != nil || patch == nil {
					return nil, errors.New("Patch metadata must be an object")
				}
				var path string
				if bytes.Equal(bytes.TrimSpace(patch["path"]), []byte("null")) || json.Unmarshal(patch["path"], &path) != nil || path == "" || utf8.RuneCountInString(path) > 4096 || strings.ContainsRune(path, 0) {
					return nil, errors.New("Patch path must contain 1 to 4096 characters without NUL")
				}
				kind, err := releaseText(patch["type"], "Patch type", 16, true)
				if err != nil {
					return nil, err
				}
				commit.PatchSet = append(commit.PatchSet, store.CommitPatch{Path: path, Type: kind})
			}
		}
		key := [2]string{commit.Repository, commit.ID}
		if seen[key] {
			return nil, errors.New("Commit repository/id pairs must be distinct")
		}
		seen[key] = true
		commit.Data = record
		result = append(result, commit)
	}
	return result, nil
}

func decodeReleaseDeploy(fields map[string]json.RawMessage) (store.ReleaseDeployInput, error) {
	var input store.ReleaseDeployInput
	var err error
	if input.Environment, err = releaseText(fields["environment"], "environment", 64, true); err != nil {
		return input, err
	}
	name, err := releaseNullableText(fields["name"], "name", 200)
	if err != nil {
		return input, err
	}
	url, err := releaseNullableText(fields["url"], "url", 2000)
	if err != nil {
		return input, err
	}
	start, err := releaseTime(fields["dateStarted"], "dateStarted")
	if err != nil {
		return input, err
	}
	finish, err := releaseTime(fields["dateFinished"], "dateFinished")
	if err != nil {
		return input, err
	}
	if start.Value != nil && finish.Value != nil && finish.Value.Before(*start.Value) {
		return input, errors.New("Deployment finish must not precede its start")
	}
	input.Name, input.URL = name.Value, url.Value
	input.DateStarted, input.DateFinished = start.Value, finish.Value
	input.Projects, err = releaseProjectRefs(fields["projects"], false)
	return input, err
}
