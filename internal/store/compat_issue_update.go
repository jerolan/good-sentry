package store

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidCompatibleIssueChange = errors.New("invalid compatible issue change")

type CompatibleIssuePolicyKind uint8

const (
	CompatibleIssueClearPolicy CompatibleIssuePolicyKind = iota
	CompatibleIssueIgnorePolicy
	CompatibleIssueReleasePolicy
	CompatibleIssueLatestReleasePolicy
	CompatibleIssueNextReleasePolicy
	CompatibleIssueClearResolutionPolicy
)

type CompatibleIssueAssignment struct{ Reference string }

type CompatibleIssuePolicy struct {
	Kind                      CompatibleIssuePolicyKind
	Version                   ReleaseVersion
	Minutes, Count, UserCount *int32
}

type CompatibleIssueChange struct {
	Status     string
	Assignment *CompatibleIssueAssignment
	Policy     *CompatibleIssuePolicy
}

func validCompatibleIssueChange(change CompatibleIssueChange) bool {
	switch change.Status {
	case "", "unresolved", "resolved", "ignored", "muted", "resolvedInNextRelease":
	default:
		return false
	}
	if a := change.Assignment; a != nil {
		if !utf8.ValidString(a.Reference) || len(a.Reference) > 254 || strings.ContainsFunc(a.Reference, unicode.IsControl) {
			return false
		}
	}
	p := change.Policy
	if p == nil {
		return true
	}
	switch p.Kind {
	case CompatibleIssueClearPolicy, CompatibleIssueClearResolutionPolicy, CompatibleIssueNextReleasePolicy, CompatibleIssueLatestReleasePolicy:
		return p.Version == "" && p.Minutes == nil && p.Count == nil && p.UserCount == nil
	case CompatibleIssueReleasePolicy:
		_, err := ParseReleaseVersion(string(p.Version))
		return err == nil && p.Minutes == nil && p.Count == nil && p.UserCount == nil
	case CompatibleIssueIgnorePolicy:
		return p.Version == "" && (p.Minutes == nil || *p.Minutes > 0) && (p.Count == nil || *p.Count > 0) && (p.UserCount == nil || *p.UserCount > 0)
	}
	return false
}

// UpdateCompatibleIssue owns scoped authorization and its response transaction;
// the actual state change is the same operation used by native issue controls.
func (s *Store) UpdateCompatibleIssue(ctx context.Context, access ManagementAccess, id string, change CompatibleIssueChange) (CompatibleIssueRecord, error) {
	var record CompatibleIssueRecord
	if access.ReadOnly {
		return record, ErrForbidden
	}
	if !canonicalIssueID(id) || !canonicalIssueID(access.UserID) || (access.OrganizationID != "" && !canonicalIssueID(access.OrganizationID)) || !validCompatibleIssueChange(change) {
		return record, ErrInvalidCompatibleIssueChange
	}
	tx, issue, org, _, err := s.issueWriteTransaction(ctx, access, id)
	if err != nil {
		return record, err
	}
	defer tx.Rollback(ctx)
	if change.Status != "" || change.Assignment != nil || change.Policy != nil {
		native, err := compatibleIssueChange(ctx, tx, org, issue, change)
		if err != nil {
			return record, err
		}
		if !validateIssueUpdate(native) {
			return record, ErrInvalidCompatibleIssueChange
		}
		if _, err = s.applyIssueUpdate(ctx, tx, access.UserID, org, issue, native); err != nil {
			return record, err
		}
	}
	record, err = readCompatibleIssueRecord(ctx, tx, access.UserID, issue.ID, CompatibleIssueDetailFilter{StatsPeriod: "24h"})
	if err != nil {
		return record, err
	}
	return record, tx.Commit(ctx)
}

func compatibleIssueChange(ctx context.Context, tx pgx.Tx, org string, issue Issue, input CompatibleIssueChange) (IssueUpdate, error) {
	change := IssueUpdate{Status: input.Status}
	if issue.Category == "feedback" && (input.Status == "resolvedInNextRelease" || input.Policy != nil &&
		(input.Policy.Kind == CompatibleIssueReleasePolicy || input.Policy.Kind == CompatibleIssueLatestReleasePolicy || input.Policy.Kind == CompatibleIssueNextReleasePolicy ||
			input.Policy.Kind == CompatibleIssueIgnorePolicy && (input.Policy.Count != nil || input.Policy.Minutes != nil || input.Policy.UserCount != nil))) {
		return change, ErrInvalidCompatibleIssueChange
	}
	if change.Status == "muted" {
		change.Status = "ignored"
	}
	if change.Status == "resolvedInNextRelease" {
		change.Status = "resolved"
	}
	if input.Assignment != nil {
		assignment, err := compatibleIssueAssignment(ctx, tx, org, input.Assignment.Reference)
		if err != nil {
			return change, err
		}
		change.Assignment = assignment
	}
	policy := input.Policy
	if input.Status == "resolvedInNextRelease" {
		if policy != nil && policy.Kind != CompatibleIssueNextReleasePolicy {
			return change, ErrInvalidCompatibleIssueChange
		}
		policy = &CompatibleIssuePolicy{Kind: CompatibleIssueNextReleasePolicy}
	}
	if policy == nil {
		return change, nil
	}
	if change.Status == "" {
		change.Status = issue.Status
	}
	switch policy.Kind {
	case CompatibleIssueClearResolutionPolicy:
		if change.Status != "resolved" {
			return change, ErrInvalidCompatibleIssueChange
		}
		change.Resolution = &IssueResolutionChange{}
	case CompatibleIssueClearPolicy:
		if change.Status == "ignored" {
			change.Ignore = &IssueIgnoreChange{}
		}
		if change.Status == "resolved" {
			change.Resolution = &IssueResolutionChange{}
		}
	case CompatibleIssueIgnorePolicy:
		if change.Status != "ignored" {
			return change, ErrInvalidCompatibleIssueChange
		}
		change.Ignore = &IssueIgnoreChange{}
		if policy.Minutes != nil || policy.Count != nil || policy.UserCount != nil {
			change.Ignore.Condition = &IssueIgnoreCondition{Minutes: policy.Minutes, Count: policy.Count, UserCount: policy.UserCount}
		}
	case CompatibleIssueReleasePolicy, CompatibleIssueLatestReleasePolicy, CompatibleIssueNextReleasePolicy:
		if change.Status != "resolved" {
			return change, ErrInvalidCompatibleIssueChange
		}
		selection := &IssueResolutionInput{Mode: "release", Version: policy.Version}
		if policy.Kind == CompatibleIssueNextReleasePolicy {
			selection.Mode = "next_release"
		}
		if policy.Kind == CompatibleIssueLatestReleasePolicy {
			if err := tx.QueryRow(ctx, `SELECT r.version FROM releases r JOIN release_projects rp ON rp.release_id=r.id
 WHERE r.organization_id=$1::bigint AND rp.project_id=$2::bigint ORDER BY r.created_at DESC,r.id DESC LIMIT 1`, org, issue.ProjectID).Scan(&selection.Version); err != nil {
				return change, dbError(err)
			}
		}
		change.Resolution = &IssueResolutionChange{Policy: selection}
	default:
		return change, ErrInvalidCompatibleIssueChange
	}
	return change, nil
}

func compatibleIssueAssignment(ctx context.Context, tx pgx.Tx, org, reference string) (*IssueAssignmentChange, error) {
	result := &IssueAssignmentChange{}
	if reference == "" {
		return result, nil
	}
	if strings.HasPrefix(reference, "user:") || strings.HasPrefix(reference, "team:") {
		kind, id, _ := strings.Cut(reference, ":")
		if !canonicalIssueID(id) {
			return nil, ErrInvalidCompatibleIssueChange
		}
		result.Target = &IssueActorRef{Type: kind, ID: id}
		return result, nil
	}
	if canonicalIssueID(reference) {
		result.Target = &IssueActorRef{Type: "user", ID: reference}
		return result, nil
	}
	numeric := strings.TrimPrefix(strings.TrimPrefix(reference, "+"), "-")
	if numeric != "" && strings.IndexFunc(numeric, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
		return nil, ErrInvalidCompatibleIssueChange
	}
	rows, err := tx.Query(ctx, `SELECT u.id::text FROM users u JOIN memberships m ON m.user_id=u.id
	WHERE m.organization_id=$1::bigint AND (lower(u.email)=lower($2) OR EXISTS(
	 SELECT 1 FROM user_emails ue WHERE ue.user_id=u.id AND ue.is_verified AND lower(ue.email)=lower($2)))
	ORDER BY u.id LIMIT 2 FOR SHARE OF m,u`, org, reference)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ErrNotFound
	}
	if len(ids) > 1 {
		return nil, ErrInvalidCompatibleIssueChange
	}
	result.Target = &IssueActorRef{Type: "user", ID: ids[0]}
	return result, nil
}
