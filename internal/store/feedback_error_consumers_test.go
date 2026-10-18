//go:build integration

package store

import (
	"encoding/json"
	"testing"
	"time"

	"goodsentry/internal/event"
)

// Error-only consumers must exclude modern feedback even when it carries an
// error level, trace and release. Generic feedback state tests have no live
// frequency rule or scoped event/trace reader, so cannot catch this leakage.
func TestModernFeedbackDoesNotCountAsAnError(t *testing.T) {
	f := resolutionStore(t)
	configuration := alertConfig("Only errors")
	configuration.Quantity, configuration.CooldownSeconds = 2, 0
	rule, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, configuration)
	if err != nil {
		t.Fatal(err)
	}
	const traceID = "0123456789abcdef0123456789abcdef"
	feedback := func(id string) event.Event {
		t.Helper()
		raw := json.RawMessage(`{"event_id":"` + id + `","type":"feedback","level":"error","release":"feedback-only@1",
 "contexts":{"trace":{"trace_id":"` + traceID + `"},"feedback":{"message":"An opinion, not another failure"}}}`)
		ev, err := event.Normalize(raw, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	first := feedback("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	feedbackIssue, _, err := f.db.Ingest(f.ctx, f.project.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	assertDeliveries := func(want int) {
		t.Helper()
		rows, _, err := f.db.AlertDeliveries(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID, 1)
		if err != nil || len(rows) != want {
			t.Fatalf("error-only deliveries=%v want=%d err=%v", rows, want, err)
		}
	}
	assertDeliveries(0)
	errorOne := groupEvent(t, 1, "feedback-error-one", "Error one", time.Now())
	groupCapture(t, f.ctx, f.db, f.project, errorOne)
	assertDeliveries(0)
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, feedback("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")); err != nil {
		t.Fatal(err)
	}
	assertDeliveries(0)
	errorTwo := groupEvent(t, 2, "feedback-error-two", "Error two", time.Now())
	groupCapture(t, f.ctx, f.db, f.project, errorTwo)
	assertDeliveries(1)
	rows, _, err := f.db.AlertDeliveries(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID, 1)
	if err != nil || rows[0].Payload.EventID != errorTwo.ID {
		t.Fatal("feedback altered the actual error crossing", rows, err)
	}
	projectEvents, err := f.db.CompatibleEvents(f.ctx, f.access, EventProjectTarget(f.project.ID), CompatibleEventFilter{Limit: 100})
	if err != nil || len(projectEvents) != 2 {
		t.Fatal("project error list included feedback", len(projectEvents), err)
	}
	for _, record := range projectEvents {
		if record.Kind == "feedback" {
			t.Fatal("project error record is feedback")
		}
	}
	issueEvents, err := f.db.CompatibleEvents(f.ctx, f.access, EventIssueTarget(feedbackIssue), CompatibleEventFilter{Limit: 25})
	if err != nil || len(issueEvents) != 1 || issueEvents[0].ID != first.ID {
		t.Fatal("feedback issue lost its own receipt", issueEvents, err)
	}
	trace, err := f.db.TraceErrors(f.ctx, f.owner.ID, f.org.Slug, traceID, f.project.ID, 1)
	if err != nil || trace.Total != 2 {
		t.Fatal("trace counted feedback as errors", trace, err)
	}
	var releases int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT count(*) FROM releases WHERE version='feedback-only@1'`).Scan(&releases); err != nil || releases != 0 {
		t.Fatal("feedback observed a release identity", releases, err)
	}
}
