package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"

	"goodsentry/internal/store"
)

func compatibleIssueNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func compatibleIssueBound(top, nested map[string]json.RawMessage, key string) (*int32, error) {
	var result *int32
	for _, fields := range []map[string]json.RawMessage{top, nested} {
		raw, present := fields[key]
		if !present {
			continue
		}
		value, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 32)
		if err != nil || value < 0 {
			return nil, errors.New(key + " must be a JSON integer between 0 and 2147483647")
		}
		number := int32(value)
		if result != nil && *result != number {
			return nil, errors.New("Top-level and statusDetails " + key + " must agree")
		}
		result = &number
	}
	if result != nil && *result == 0 {
		return nil, nil
	}
	return result, nil
}

// Presence has meaning here: an omitted policy preserves it, an empty policy
// clears it, and an explicit zero disables one mute bound. Store validates the
// typed intent against the locked issue's effective status and current targets.
func parseCompatibleIssueChange(fields map[string]json.RawMessage) (store.CompatibleIssueChange, error) {
	var change store.CompatibleIssueChange
	if raw, present := fields["status"]; present && !compatibleIssueNull(raw) {
		if json.Unmarshal(raw, &change.Status) != nil {
			return change, errors.New("Status must be a string or null")
		}
		switch change.Status {
		case "unresolved", "resolved", "ignored", "muted", "resolvedInNextRelease":
		default:
			return change, errors.New("Status must be unresolved, resolved, ignored, muted or resolvedInNextRelease")
		}
	}
	if raw, present := fields["assignedTo"]; present {
		change.Assignment = &store.CompatibleIssueAssignment{}
		if !compatibleIssueNull(raw) && json.Unmarshal(raw, &change.Assignment.Reference) != nil {
			return change, errors.New("assignedTo must be a string or null")
		}
	}
	var details map[string]json.RawMessage
	raw, policyPresent := fields["statusDetails"]
	if policyPresent && !compatibleIssueNull(raw) {
		var err error
		details, err = issueObject(raw, "inRelease", "inNextRelease", "ignoreDuration", "ignoreCount", "ignoreUserCount", "ignoreWindow")
		if err != nil {
			return change, err
		}
	}
	_, releasePresent := details["inRelease"]
	_, nextPresent := details["inNextRelease"]
	_, topMinutes := fields["ignoreDuration"]
	_, topCount := fields["ignoreCount"]
	_, topUsers := fields["ignoreUserCount"]
	_, topWindow := fields["ignoreWindow"]
	_, nestedMinutes := details["ignoreDuration"]
	_, nestedCount := details["ignoreCount"]
	_, nestedUsers := details["ignoreUserCount"]
	_, nestedWindow := details["ignoreWindow"]
	mutePresent := topMinutes || topCount || topUsers || topWindow || nestedMinutes || nestedCount || nestedUsers || nestedWindow
	if (releasePresent || nextPresent) && mutePresent {
		return change, errors.New("Resolution selectors and ignore conditions cannot be combined")
	}
	if mutePresent {
		minutes, err := compatibleIssueBound(fields, details, "ignoreDuration")
		if err != nil {
			return change, err
		}
		count, err := compatibleIssueBound(fields, details, "ignoreCount")
		if err != nil {
			return change, err
		}
		users, err := compatibleIssueBound(fields, details, "ignoreUserCount")
		if err != nil {
			return change, err
		}
		window, err := compatibleIssueBound(fields, details, "ignoreWindow")
		if err != nil {
			return change, err
		}
		if window != nil && *window > 10080 {
			return change, errors.New("ignoreWindow must be between 0 and 10080 minutes")
		}
		if window != nil && count == nil {
			return change, errors.New("ignoreWindow requires a positive ignoreCount")
		}
		change.Policy = &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueIgnorePolicy, Minutes: minutes, Count: count, UserCount: users, Window: window}
		return change, nil
	}
	if !policyPresent {
		return change, nil
	}
	var version string
	if raw, present := details["inRelease"]; present && !compatibleIssueNull(raw) {
		if json.Unmarshal(raw, &version) != nil {
			return change, errors.New("inRelease must be a string or null")
		}
	}
	var next bool
	if raw, present := details["inNextRelease"]; present && !compatibleIssueNull(raw) {
		if json.Unmarshal(raw, &next) != nil {
			return change, errors.New("inNextRelease must be a boolean or null")
		}
	}
	if next && version != "" {
		return change, errors.New("inNextRelease and a nonempty inRelease cannot be combined")
	}
	policy := &store.CompatibleIssuePolicy{Kind: store.CompatibleIssueClearPolicy}
	if releasePresent && !compatibleIssueNull(details["inRelease"]) || nextPresent && !compatibleIssueNull(details["inNextRelease"]) {
		policy.Kind = store.CompatibleIssueClearResolutionPolicy
	}
	switch {
	case next:
		policy.Kind = store.CompatibleIssueNextReleasePolicy
	case version == "latest":
		policy.Kind = store.CompatibleIssueLatestReleasePolicy
	case version != "":
		var err error
		policy.Kind = store.CompatibleIssueReleasePolicy
		policy.Version, err = store.ParseReleaseVersion(version)
		if err != nil {
			return change, err
		}
	}
	change.Policy = policy
	return change, nil
}
