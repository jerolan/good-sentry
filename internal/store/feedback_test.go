//go:build integration

package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func modernFeedbackEvent(t *testing.T, id, message, associated string) event.Event {
	t.Helper()
	body := map[string]any{"type": "feedback", "event_id": id, "fingerprint": []string{"shared-custom-fingerprint"}, "contexts": map[string]any{"feedback": map[string]any{"message": message, "name": "SDK reporter", "contact_email": "sdk@example.test", "url": "https://example.test/source", "associated_event_id": associated, "extension": map[string]any{"kept": true}}}, "extra": map[string]any{"bytes": "AAEC/w=="}}
	body["release"] = "feedback-release-must-not-exist"
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func modernErrorEvent(t *testing.T, n int, fingerprint string) event.Event {
	t.Helper()
	return groupEvent(t, n, fingerprint, "error "+fmt.Sprint(n), time.Now().UTC())
}

func modernErrorWithID(t *testing.T, id string) event.Event {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"event_id": id, "message": "associated error", "level": "error"})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestModernFeedbackOwnsReceiptAndRetry(t *testing.T) {
	f := resolutionStore(t)
	ctx, db := f.ctx, f.db
	feedbackID := "0123456789abcdef0123456789abcdef"
	if _, err := db.pool.Exec(ctx, `INSERT INTO user_reports(project_id,event_id,name,email,comments) VALUES($1,$2,'legacy','legacy@example.test','legacy report')`, f.project.ID, feedbackID); err != nil {
		t.Fatal(err)
	}
	ev := modernFeedbackEvent(t, feedbackID, "same words", "")
	attachmentBytes := []byte{0, 255, 1, 128}
	issue, fresh, err := db.Ingest(ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "feedback.bin", ContentType: "application/octet-stream", Data: attachmentBytes}}})
	if err != nil || !fresh || issue == "" {
		t.Fatalf("feedback capture issue=%s fresh=%v err=%v", issue, fresh, err)
	}
	other := modernFeedbackEvent(t, "1123456789abcdef0123456789abcdef", "same words", "")
	otherIssue, fresh, err := db.Ingest(ctx, f.project.ID, other)
	if err != nil || !fresh || otherIssue == issue {
		t.Fatalf("literal identity grouping issue=%s first=%s fresh=%v err=%v", otherIssue, issue, fresh, err)
	}
	var category string
	var count, receipts int64
	if err := db.pool.QueryRow(ctx, `SELECT category,count,receipt_count FROM issues WHERE id=$1::bigint`, issue).Scan(&category, &count, &receipts); err != nil {
		t.Fatal(err)
	}
	if category != "feedback" || count != 1 || receipts != 1 {
		t.Fatalf("feedback issue state category=%s count=%d receipt_count=%d", category, count, receipts)
	}
	var gotID, message string
	var raw []byte
	var firstReceived time.Time
	if err := db.pool.QueryRow(ctx, `SELECT event_id,data,created_at FROM events WHERE event_id=$1`, feedbackID).Scan(&gotID, &raw, &firstReceived); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	gotID, _ = payload["event_id"].(string)
	fb := payload["contexts"].(map[string]any)["feedback"].(map[string]any)
	message, _ = fb["message"].(string)
	if gotID != feedbackID || message != "same words" || fb["name"] != "SDK reporter" || fb["extension"].(map[string]any)["kept"] != true || payload["extra"].(map[string]any)["bytes"] != "AAEC/w==" || payload["release"] != "feedback-release-must-not-exist" {
		t.Fatalf("raw feedback payload changed: %s", raw)
	}
	var receipt int64
	if err := db.pool.QueryRow(ctx, `SELECT event_receipt_id FROM feedback_events WHERE event_id=$1`, feedbackID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if receipt <= 0 {
		t.Fatalf("invalid feedback receipt %d", receipt)
	}
	retryEvent := modernFeedbackEvent(t, feedbackID, "changed duplicate payload", "")
	if id, admitted, err := db.Ingest(ctx, f.project.ID, retryEvent); err != nil || admitted || id != issue {
		t.Fatalf("retry issue=%s admitted=%v err=%v", id, admitted, err)
	}
	var storedReceipt int64
	if err := db.pool.QueryRow(ctx, `SELECT event_receipt_id FROM feedback_events WHERE event_id=$1`, feedbackID).Scan(&storedReceipt); err != nil || storedReceipt != receipt {
		t.Fatalf("retry changed receipt %d to %d: %v", receipt, storedReceipt, err)
	}
	var retryRaw []byte
	var retryReceived time.Time
	if err := db.pool.QueryRow(ctx, `SELECT data,created_at FROM events WHERE event_id=$1`, feedbackID).Scan(&retryRaw, &retryReceived); err != nil || !reflect.DeepEqual(retryRaw, raw) || !retryReceived.Equal(firstReceived) {
		t.Fatalf("retry changed first payload or receipt clock: %s %v %v", retryRaw, retryReceived, err)
	}
	var storedBytes []byte
	if err := db.pool.QueryRow(ctx, `SELECT data FROM attachments WHERE event_id=$1 AND filename='feedback.bin'`, feedbackID).Scan(&storedBytes); err != nil || !reflect.DeepEqual(storedBytes, attachmentBytes) {
		t.Fatalf("feedback binary child=%s err=%v", base64.StdEncoding.EncodeToString(storedBytes), err)
	}
	var reports int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM user_reports WHERE event_id=$1`, feedbackID).Scan(&reports); err != nil {
		t.Fatal(err)
	}
	if reports != 1 {
		t.Fatalf("feedback duplicated legacy reports: %d", reports)
	}
	var legacyReceipt any
	if err := db.pool.QueryRow(ctx, `SELECT event_receipt_id FROM user_reports WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, feedbackID).Scan(&legacyReceipt); err != nil || legacyReceipt != nil {
		t.Fatalf("modern feedback bound a legacy report: receipt=%v err=%v", legacyReceipt, err)
	}
	errIssue := groupCapture(t, ctx, db, f.project, modernErrorEvent(t, 31, "feedback-error-control"))
	var errorReceipts int64
	if err := db.pool.QueryRow(ctx, `SELECT receipt_count FROM issues WHERE id=$1::bigint`, errIssue).Scan(&errorReceipts); err != nil || errorReceipts != 1 {
		t.Fatalf("native error receipt_count=%d err=%v", errorReceipts, err)
	}
}

func TestModernFeedbackAssociationPinsFirstCompatibleReceipt(t *testing.T) {
	f := resolutionStore(t)
	ctx, db := f.ctx, f.db
	id := "2123456789abcdef0123456789abcdef"
	feedback, fresh, err := db.Ingest(ctx, f.project.ID, modernFeedbackEvent(t, id, "pending message", "3123456789abcdef0123456789abcdef"))
	if err != nil || !fresh {
		t.Fatalf("pending feedback issue=%s fresh=%v err=%v", feedback, fresh, err)
	}
	var pending bool
	var bound any
	if err := db.pool.QueryRow(ctx, `SELECT association_pending,associated_event_receipt_id FROM feedback_events WHERE event_id=$1`, id).Scan(&pending, &bound); err != nil || !pending || bound != nil {
		t.Fatalf("initial association pending=%v receipt=%v err=%v", pending, bound, err)
	}
	target := "3123456789abcdef0123456789abcdef"
	_, fresh, err = db.Ingest(ctx, f.project.ID, modernErrorWithID(t, "3123456789abcdef0123456789abcdef"))
	if err != nil || !fresh {
		t.Fatalf("late error fresh=%v err=%v", fresh, err)
	}
	var targetReceipt int64
	if err := db.pool.QueryRow(ctx, `SELECT id FROM events WHERE event_id=$1`, target).Scan(&targetReceipt); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT association_pending,associated_event_receipt_id FROM feedback_events WHERE event_id=$1`, id).Scan(&pending, &bound); err != nil || pending || bound == nil || bound.(int64) != targetReceipt {
		t.Fatalf("late binding pending=%v receipt=%v want=%d err=%v", pending, bound, targetReceipt, err)
	}
	// A legacy report referencing the same SDK identity has no binding path from modern feedback.
	other, err := db.CreateProject(ctx, f.owner.ID, f.org.Slug, "Feedback foreign project", "go")
	if err != nil {
		t.Fatal(err)
	}
	foreign := modernFeedbackEvent(t, "5123456789abcdef0123456789abcdef", "foreign ref", target)
	foreignIssue, fresh, err := db.Ingest(ctx, other.ID, foreign)
	if err != nil || !fresh {
		t.Fatalf("foreign reference capture issue=%s fresh=%v err=%v", foreignIssue, fresh, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT associated_event_receipt_id,association_pending FROM feedback_events WHERE event_id=$1`, foreign.ID).Scan(&bound, &pending); err != nil || bound != nil || !pending {
		t.Fatalf("foreign project linked or became pending: receipt=%v pending=%v err=%v", bound, pending, err)
	}
	transactionID := "6123456789abcdef0123456789abcdef"
	transactionRaw := json.RawMessage(`{"event_id":"` + transactionID + `","type":"transaction","transaction":"/feedback-target","start_timestamp":"2026-01-01T00:00:00Z","timestamp":"2026-01-01T00:00:01Z"}`)
	transaction, err := event.Normalize(transactionRaw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, admitted, err := db.Ingest(ctx, f.project.ID, transaction); err != nil || !admitted {
		t.Fatalf("transaction target admitted=%v err=%v", admitted, err)
	}
	transactionFeedback := modernFeedbackEvent(t, "7123456789abcdef0123456789abcdef", "transaction ref", transactionID)
	if _, fresh, err := db.Ingest(ctx, f.project.ID, transactionFeedback); err != nil || !fresh {
		t.Fatalf("transaction feedback fresh=%v err=%v", fresh, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT associated_event_receipt_id,association_pending FROM feedback_events WHERE event_id=$1`, transactionFeedback.ID).Scan(&bound, &pending); err != nil || bound != nil || pending {
		t.Fatalf("known transaction target became linkable or pending: receipt=%v pending=%v err=%v", bound, pending, err)
	}
	// Release metadata is owned by error/message receipts only.
	feedbackWithRelease := modernFeedbackEvent(t, "8123456789abcdef0123456789abcdef", "release ignored", "")
	if _, _, err := db.Ingest(ctx, f.project.ID, feedbackWithRelease); err != nil {
		t.Fatal(err)
	}
	var releases int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM releases WHERE organization_id=$1::bigint AND version=$2`, f.org.ID, feedbackWithRelease.Release).Scan(&releases); err != nil || releases != 0 {
		t.Fatalf("feedback created release metadata rows=%d err=%v", releases, err)
	}
	// Established numeric identity is removed on target deletion; a recreated SDK ID stays unrelated.
	if _, err := db.pool.Exec(ctx, `DELETE FROM events WHERE id=$1`, targetReceipt); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT associated_event_receipt_id,association_pending FROM feedback_events WHERE event_id=$1`, id).Scan(&bound, &pending); err != nil || bound != nil || pending {
		t.Fatalf("deleted target association receipt=%v pending=%v err=%v", bound, pending, err)
	}
	_, fresh, err = db.Ingest(ctx, f.project.ID, modernErrorWithID(t, "3123456789abcdef0123456789abcdef"))
	if err != nil || !fresh {
		t.Fatalf("recreated error fresh=%v err=%v", fresh, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT associated_event_receipt_id,association_pending FROM feedback_events WHERE event_id=$1`, id).Scan(&bound, &pending); err != nil || bound != nil || pending {
		t.Fatalf("recreated SDK ID rebound: receipt=%v pending=%v err=%v", bound, pending, err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM events WHERE event_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	var ownMetadata int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM feedback_events WHERE event_id=$1`, id).Scan(&ownMetadata); err != nil || ownMetadata != 0 {
		t.Fatalf("feedback deletion left own metadata rows=%d err=%v", ownMetadata, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE event_id=$1`, "3123456789abcdef0123456789abcdef").Scan(&ownMetadata); err != nil || ownMetadata != 1 {
		t.Fatalf("feedback deletion affected associated error rows=%d err=%v", ownMetadata, err)
	}
}

func TestBindPendingFeedbackAssociationsDrainsBoundedBatches(t *testing.T) {
	f := resolutionStore(t)
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Minute)
	defer cancel()
	db := f.db
	target := "6123456789abcdef0123456789abcdef"
	for n := 0; n < 601; n++ {
		id := fmt.Sprintf("%032x", 1000+n)
		_, fresh, err := db.Ingest(ctx, f.project.ID, modernFeedbackEvent(t, id, "batch", target))
		if err != nil || !fresh {
			t.Fatalf("seed %d fresh=%v err=%v", n, fresh, err)
		}
	}
	for _, state := range []struct {
		id         string
		associated string
	}{{"7123456789abcdef0123456789abcdef", "9123456789abcdef0123456789abcdef"}, {"8123456789abcdef0123456789abcdef", "9123456789abcdef0123456789abcdef"}} {
		_, _, err := db.Ingest(ctx, f.project.ID, modernFeedbackEvent(t, state.id, "state", state.associated))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := db.Ingest(ctx, f.project.ID, modernFeedbackEvent(t, "a123456789abcdef0123456789abcdef", "held", target)); err != nil {
		t.Fatal(err)
	} // eligible but feedback row is held below
	var held int64
	if err := db.pool.QueryRow(ctx, `SELECT event_receipt_id FROM feedback_events WHERE event_id=$1`, "a123456789abcdef0123456789abcdef").Scan(&held); err != nil {
		t.Fatal(err)
	}
	locker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback(ctx)
	var locked int64
	if err := locker.QueryRow(ctx, `SELECT event_receipt_id FROM feedback_events WHERE event_receipt_id=$1 FOR UPDATE`, held).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	if locked != held {
		t.Fatalf("locked feedback receipt=%d want=%d", locked, held)
	}
	if _, fresh, err := db.Ingest(ctx, f.project.ID, modernErrorWithID(t, target)); err != nil || !fresh {
		t.Fatalf("target capture fresh=%v err=%v", fresh, err)
	}
	var fastBound int64
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM feedback_events WHERE associated_event_id=$1 AND associated_event_receipt_id IS NOT NULL`, target).Scan(&fastBound); err != nil || fastBound != 250 {
		t.Fatalf("parent fast path bindings=%d want=250 err=%v", fastBound, err)
	}
	// Re-pend the parent's real first batch to exercise the catch-up's separate
	// bounded drain while the locked feedback row remains unavailable.
	if _, err := db.pool.Exec(ctx, `UPDATE feedback_events SET associated_event_receipt_id=NULL,association_pending=true WHERE associated_event_id=$1 AND event_id LIKE '0000000000000000000000000000%'`, target); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{250, 250, 101} {
		got, err := db.BindPendingFeedbackAssociations(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("binding batch=%d want=%d", got, want)
		}
	}
	if got, err := db.BindPendingFeedbackAssociations(ctx); err != nil || got != 0 {
		t.Fatalf("held-only pass bindings=%d err=%v", got, err)
	}
	if err := locker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := db.BindPendingFeedbackAssociations(ctx); err != nil || got != 1 {
		t.Fatalf("released held candidate bindings=%d err=%v", got, err)
	}
	var boundCount int64
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM feedback_events WHERE association_pending=false AND associated_event_receipt_id IS NOT NULL`).Scan(&boundCount); err != nil || boundCount != 602 {
		t.Fatalf("eligible associations bound=%d want=602 err=%v", boundCount, err)
	}
	for _, id := range []string{"7123456789abcdef0123456789abcdef", "8123456789abcdef0123456789abcdef"} {
		var pending bool
		var receipt any
		if err := db.pool.QueryRow(ctx, `SELECT association_pending,associated_event_receipt_id FROM feedback_events WHERE event_id=$1`, id).Scan(&pending, &receipt); err != nil || !pending || receipt != nil {
			t.Fatalf("missing target candidate changed id=%s pending=%v receipt=%v err=%v", id, pending, receipt, err)
		}
	}
}

func TestFeedbackCategoryMergeAndMixedBulkRollback(t *testing.T) {
	f := resolutionStore(t)
	ctx, db := f.ctx, f.db
	feedbackA, _, err := db.Ingest(ctx, f.project.ID, modernFeedbackEvent(t, "b123456789abcdef0123456789abcdef", "A", ""))
	if err != nil {
		t.Fatal(err)
	}
	feedbackB, _, err := db.Ingest(ctx, f.project.ID, modernFeedbackEvent(t, "c123456789abcdef0123456789abcdef", "B", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.MergeIssues(ctx, f.owner.ID, f.org.Slug, []string{feedbackA, feedbackB}); err != nil {
		t.Fatalf("same-category feedback merge: %v", err)
	}
	if _, err := db.UnmergeIssue(ctx, f.owner.ID, feedbackA, []string{feedbackB}); err != nil {
		t.Fatalf("same-category feedback unmerge: %v", err)
	}
	errIssue := groupCapture(t, ctx, db, f.project, modernErrorEvent(t, 36, "category-mix-error"))
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	if _, err := db.MergeIssues(ctx, f.owner.ID, f.org.Slug, []string{feedbackA, errIssue}); err != ErrInvalidIssueChange {
		t.Fatalf("native cross-category merge error=%v", err)
	}
	groupRequireUnchanged(t, ctx, db, before)
	access := f.access
	filter := CompatibleIssueBulkFilter{IDs: []string{feedbackA, errIssue}, Limit: 10}
	command := CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}, Merge: true}
	if _, err := db.UpdateCompatibleIssues(ctx, access, f.project.ID, filter, command); !errors.Is(err, ErrInvalidCompatibleIssueChange) && !errors.Is(err, ErrInvalidIssueChange) {
		t.Fatalf("compatible mixed bulk error=%v", err)
	}
	groupRequireUnchanged(t, ctx, db, before)
}
