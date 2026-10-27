//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"goodsentry/internal/event"
)

// Store owns the atomic cut across active routes, capture receipts and quota.
// HTTP tests cannot prove that detached signatures remain usable or that a
// throttled capture is acknowledged without storing parent data.
func TestDiscardCompatibleIssueTombstonesOnlyAttachedErrorFingerprints(t *testing.T) {
	f := resolutionStore(t)
	aEvent := groupEvent(t, 811, "discard-detached", "Discard detached", time.Now())
	bEvent := groupEvent(t, 812, "discard-attached", "Discard attached", time.Now())
	parts := CaptureItems{
		Attachments:   []CaptureAttachment{{Filename: "discard.bin", ContentType: "application/octet-stream", Data: []byte{0, 255}}},
		EnvelopeItems: []CaptureEnvelopeItem{{Type: "parented", Header: json.RawMessage(`{"type":"parented"}`), Data: []byte{255, 0}}},
	}
	a, fresh, err := f.db.Ingest(f.ctx, f.project.ID, aEvent, parts)
	if err != nil || !fresh {
		t.Fatalf("first capture: issue=%s fresh=%v err=%v", a, fresh, err)
	}
	b, fresh, err := f.db.Ingest(f.ctx, f.project.ID, bEvent, parts)
	if err != nil || !fresh {
		t.Fatalf("second capture: issue=%s fresh=%v err=%v", b, fresh, err)
	}
	merged, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b})
	if err != nil || !merged.Changed {
		t.Fatalf("merge captures: result=%+v err=%v", merged, err)
	}
	root := merged.Issue.ID
	member := a
	if member == root {
		member = b
	}
	var alias *IssueMergedError
	if err := f.db.DiscardCompatibleIssue(f.ctx, f.access, member); !errors.As(err, &alias) || alias.MergedInto != root {
		t.Fatalf("discard alias error=%v root=%s", err, root)
	}

	hashes := hashSamples(t, f, root, CompatibleIssueHashFilter{Limit: 100})
	detachedHash := hashForSample(t, hashes, aEvent.ID)
	if err := f.db.ResetCompatibleIssueHashes(f.ctx, f.access, root, []string{detachedHash}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.DiscardCompatibleIssue(f.ctx, f.access, root); err != nil {
		t.Fatal(err)
	}
	var tombstones []string
	rows, err := f.db.pool.Query(f.ctx, `SELECT fingerprint FROM discarded_issue_hashes WHERE project_id=$1::bigint ORDER BY fingerprint`, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var fingerprint string
		if err := rows.Scan(&fingerprint); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		tombstones = append(tombstones, fingerprint)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(tombstones) != 1 || tombstones[0] != bEvent.Fingerprint {
		t.Fatalf("discarded fingerprints=%v; want only the still-attached signature %q", tombstones, bEvent.Fingerprint)
	}
	databaseURL := f.db.pool.Config().ConnString()
	f.db.Close()
	reopened, err := Open(f.ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	f.db = reopened
	var originalRows int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT
 (SELECT count(*) FROM events WHERE project_id=$1::bigint AND event_id=ANY($2::text[]))+
 (SELECT count(*) FROM attachments WHERE project_id=$1::bigint AND event_id=ANY($2::text[]))+
 (SELECT count(*) FROM envelope_items WHERE project_id=$1::bigint AND event_id=ANY($2::text[]))`,
		f.project.ID, []string{aEvent.ID, bEvent.ID}).Scan(&originalRows); err != nil || originalRows != 0 {
		t.Fatalf("discard retained parent or child rows=%d err=%v", originalRows, err)
	}

	discarded, err := f.db.ingest(f.ctx, f.project.ID, groupEvent(t, 813, "discard-attached", "Repeated error", time.Now()), nil)
	if err != nil || !discarded.Discarded || discarded.Fresh || discarded.IssueID != "" {
		t.Fatalf("attached fingerprint capture=%+v err=%v", discarded, err)
	}
	cspEvents, err := event.NormalizeSecurityReports([]byte(`{"csp-report":{"document-uri":"https://app.example/","blocked-uri":"https://bad.example/script.js","violated-directive":"script-src"}}`), time.Now(), event.SecurityReportOptions{})
	if err != nil || len(cspEvents) != 1 {
		t.Fatalf("normalize CSP capture: events=%+v err=%v", cspEvents, err)
	}
	cspEvents[0].Fingerprint = bEvent.Fingerprint
	csp, err := f.db.ingest(f.ctx, f.project.ID, cspEvents[0], nil)
	if err != nil || !csp.Discarded || csp.Fresh || csp.IssueID != "" {
		t.Fatalf("discarded CSP fingerprint capture=%+v err=%v", csp, err)
	}
	detached, err := f.db.ingest(f.ctx, f.project.ID, groupEvent(t, 814, "discard-detached", "Detached error can return", time.Now()), nil)
	if err != nil || detached.Discarded || !detached.Fresh || detached.IssueID == "" {
		t.Fatalf("detached fingerprint capture=%+v err=%v", detached, err)
	}
	feedbackRaw := json.RawMessage(`{"event_id":"00000000000000000000000000000815","type":"feedback","fingerprint":["discard-attached"],"contexts":{"feedback":{"message":"Feedback stays separate"}}}`)
	feedback, err := event.Normalize(feedbackRaw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	feedbackOutcome, err := f.db.ingest(f.ctx, f.project.ID, feedback, nil)
	if err != nil || feedbackOutcome.Discarded || !feedbackOutcome.Fresh || feedbackOutcome.IssueID == "" {
		t.Fatalf("feedback sharing a fingerprint=%+v err=%v", feedbackOutcome, err)
	}
	if err := f.db.DiscardCompatibleIssue(f.ctx, f.access, feedbackOutcome.IssueID); !errors.Is(err, ErrInvalidCompatibleIssueChange) {
		t.Fatalf("feedback permanent discard error=%v", err)
	}
	otherProject, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other discard scope", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	foreign, fresh, err := f.db.Ingest(f.ctx, otherProject.ID, groupEvent(t, 816, "discard-attached", "Other project still accepts errors", time.Now()))
	if err != nil || !fresh || foreign == "" {
		t.Fatalf("same fingerprint in another project: issue=%s fresh=%v err=%v", foreign, fresh, err)
	}
}

func TestDiscardCompatibleIssueCutsAfterInFlightCapture(t *testing.T) {
	f := resolutionStore(t)
	old := groupEvent(t, 826, "discard-items-before", "Original receipt", time.Now().Add(-time.Hour))
	root, _, err := f.db.Ingest(f.ctx, f.project.ID, old, deleteOrderParts("old"))
	if err != nil || root == "" {
		t.Fatalf("initial capture root=%s err=%v", root, err)
	}
	barrier, barrierPID := captureItemsBarrier(t, f)
	if _, err = barrier.Exec(f.ctx, "LOCK TABLE attachments IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	capturer, discarder := captureItemsOther(t, f), captureItemsOther(t, f)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	next := groupEvent(t, 827, "discard-items-before", "In-flight receipt", old.Timestamp.Add(time.Second))
	captureDone := make(chan captureItemsResult, 1)
	go func() {
		id, fresh, err := capturer.Ingest(ctx, f.project.ID, next, deleteOrderParts("inflight"))
		captureDone <- captureItemsResult{id, fresh, err}
	}()
	capturePID := groupWaitBlocked(t, f.ctx, barrier, barrierPID, 1)[0]
	discardDone := make(chan error, 1)
	go func() { discardDone <- discarder.DiscardCompatibleIssue(ctx, f.access, root) }()
	deleteOrderGateWait(t, f, barrier, capturePID, f.project.ID, "ExclusiveLock")
	if err = barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if result := captureItemsAwait(t, captureDone); result.err != nil || !result.fresh || result.issue != root {
		t.Fatalf("capture preceding discard did not commit to its existing group: %+v", result)
	}
	deleteOrderAwait(t, ctx, discardDone)
	deleteOrderRows(t, f, 0, 0, 0, 0)
	late, err := f.db.ingest(ctx, f.project.ID, groupEvent(t, 828, "discard-items-before", "Capture after discard", time.Now()), nil, deleteOrderParts("late"))
	if err != nil || !late.Discarded || late.Fresh || late.IssueID != "" {
		t.Fatalf("capture after discard=%+v err=%v", late, err)
	}
	deleteOrderRows(t, f, 0, 0, 0, 0)
}

func TestDiscardedCaptureSkipsParentThrottleAndChargesAdmittedSiblings(t *testing.T) {
	f := resolutionStore(t)
	keys, err := f.db.CompatibleProjectKeys(f.ctx, f.access, f.project.ID, nil, 0, 100)
	if err != nil || len(keys) != 1 {
		t.Fatalf("initial project key=%+v err=%v", keys, err)
	}
	// These assertions compare several charges in one fixed window. Choose a
	// valid window with ample time remaining, using the same clock as admission;
	// crossing a boundary legitimately resets usage and invalidates the fixture.
	var epoch int64
	if err := f.db.pool.QueryRow(f.ctx, `SELECT floor(extract(epoch FROM clock_timestamp()))::bigint`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	window := int64(604800)
	for window > 604000 && window-epoch%window < 1200 {
		window--
	}
	if window <= 604000 {
		t.Fatal("could not choose a stable rate window")
	}
	limited, err := f.db.UpdateCompatibleProjectKey(f.ctx, f.access, f.project.ID, keys[0].ID, "discard admission", &CompatibleKeyRateLimit{Window: int(window), Count: 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := groupEvent(t, 821, "discard-quota", "Quota issue", time.Now())
	initial, err := f.db.IngestWithAdmission(f.ctx, f.project.ID, first, CaptureAdmission{PublicKey: limited.Public, Charge: 1})
	if err != nil || !initial.Fresh || initial.IssueID == "" {
		t.Fatalf("initial charged capture=%+v err=%v", initial, err)
	}
	if err := f.db.DiscardCompatibleIssue(f.ctx, f.access, initial.IssueID); err != nil {
		t.Fatal(err)
	}
	mixed := groupEvent(t, 822, "discard-quota", "Matching error with independent spans and logs", time.Now())
	outcome, err := f.db.IngestWithAdmission(f.ctx, f.project.ID, mixed, CaptureAdmission{
		PublicKey: limited.Public, Charge: 4, IndependentCharge: 3, Throttle: true,
	})
	if err != nil || outcome.Discarded || !outcome.ProjectThrottled || outcome.RetryAfter != 0 {
		t.Fatalf("discarded mixed capture must retain request throttle: outcome=%+v err=%v", outcome, err)
	}
	var used int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count FROM project_key_rate_usage WHERE api_id=$1::uuid`, limited.ID).Scan(&used); err != nil || used != 1 {
		t.Fatalf("throttled mixed capture quota used=%d want initial event charge 1 err=%v", used, err)
	}
	last := groupEvent(t, 823, "discard-quota", "Discarded error with no siblings", time.Now())
	free, err := f.db.IngestWithAdmission(f.ctx, f.project.ID, last, CaptureAdmission{
		PublicKey: limited.Public, Charge: 1, Throttle: true,
	})
	if err != nil || !free.Discarded || free.ProjectThrottled || free.RetryAfter != 0 {
		t.Fatalf("discarded event-only outcome=%+v err=%v", free, err)
	}
	acceptedMixed := groupEvent(t, 824, "discard-quota", "Matching error with admitted independent spans and logs", time.Now())
	accepted, err := f.db.IngestWithAdmission(f.ctx, f.project.ID, acceptedMixed, CaptureAdmission{
		PublicKey: limited.Public, Charge: 4, IndependentCharge: 3,
	})
	if err != nil || !accepted.Discarded || accepted.ProjectThrottled || accepted.RetryAfter != 0 {
		t.Fatalf("unthrottled mixed discarded capture=%+v err=%v", accepted, err)
	}
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count FROM project_key_rate_usage WHERE api_id=$1::uuid`, limited.ID).Scan(&used); err != nil || used != 4 {
		t.Fatalf("quota used=%d want independent sibling charges to bring total to 4 err=%v", used, err)
	}
	overLimit := groupEvent(t, 825, "discard-quota", "Matching error whose siblings exceed quota", time.Now())
	limitedOutcome, err := f.db.IngestWithAdmission(f.ctx, f.project.ID, overLimit, CaptureAdmission{
		PublicKey: limited.Public, Charge: 5, IndependentCharge: 4,
	})
	if err != nil || limitedOutcome.Discarded || limitedOutcome.RetryAfter <= 0 || limitedOutcome.ProjectThrottled {
		t.Fatalf("discarded siblings over quota=%+v err=%v", limitedOutcome, err)
	}
	var eventRows int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE project_id=$1::bigint AND event_id=ANY($2::text[])`, f.project.ID,
		[]string{first.ID, mixed.ID, last.ID, acceptedMixed.ID, overLimit.ID}).Scan(&eventRows); err != nil || eventRows != 0 {
		t.Fatalf("discarded event receipts=%d err=%v", eventRows, err)
	}
}
