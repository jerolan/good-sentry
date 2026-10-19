//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestModernFeedbackEnvelopeHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	path := "/api/" + f.project.ID + "/envelope/"
	header := http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=" + f.project.PublicKey}}
	const feedbackID = "b1000000000000000000000000000001"
	const associatedID = "abcdef0123456789abcdef0123456789"
	wire := `{"event_id":"b1000000000000000000000000000001","sdk":{"name":"sentry.javascript.node","version":"11.7.0"}}` + "\n" +
		`{"type":"feedback"}` + "\n" +
		`{"contexts":{"feedback":{"message":"Associated feedback\nSecond line","name":"SDK name","contact_email":"sdk@example.invalid","associated_event_id":"abcdef0123456789abcdef0123456789"},"runtime":{"name":"node","version":"v24.19.0"}},"type":"feedback","platform":"node","event_id":"b1000000000000000000000000000001","sdk":{"name":"sentry.javascript.node","version":"11.7.0"}}` + "\n" +
		`{"type":"attachment","length":8,"filename":"feedback-proof.bin","content_type":"application/octet-stream"}` + "\n" + string([]byte{0, 255, 1, 254, 2, 0, 128, 42})
	var ack struct {
		ID               string
		UnsupportedItems []string
	}
	decodeAck := func(body []byte, wantID string) {
		t.Helper()
		if err := json.Unmarshal(body, &ack); err != nil || ack.ID != wantID || len(ack.UnsupportedItems) != 0 {
			t.Fatalf("feedback was not acknowledged as a supported parent: %+v, %v", ack, err)
		}
	}
	decodeAck(f.request(t, http.DefaultClient, "POST", path, wire, header, 200), feedbackID)

	var row struct {
		Kind, EventID, Raw, Level string
		Created                   time.Time
		Name, Email               string
		Association               string
	}
	readFeedback := func() {
		t.Helper()
		if err := f.sql.QueryRow(f.ctx, `SELECT e.kind,e.event_id,e.data::text,e.level,e.created_at,e.data#>>'{contexts,feedback,name}',e.data#>>'{contexts,feedback,contact_email}',COALESCE(f.associated_event_id,'')
FROM events e JOIN feedback_events f ON f.event_receipt_id=e.id WHERE e.project_id=$1::bigint AND e.event_id=$2`, f.project.ID, feedbackID).
			Scan(&row.Kind, &row.EventID, &row.Raw, &row.Level, &row.Created, &row.Name, &row.Email, &row.Association); err != nil {
			t.Fatal(err)
		}
	}
	readFeedback()
	if row.Kind != "feedback" || row.EventID != feedbackID || row.Name != "SDK name" || row.Email != "sdk@example.invalid" || row.Association != associatedID || !strings.Contains(row.Raw, "Associated feedback\\nSecond line") || row.Level != "info" {
		t.Fatalf("unexpected feedback receipt: %+v", row)
	}
	var attachment []byte
	if err := f.sql.QueryRow(f.ctx, `SELECT a.data FROM attachments a WHERE a.project_id=$1::bigint AND a.event_id=$2 AND a.filename='feedback-proof.bin'`, f.project.ID, feedbackID).Scan(&attachment); err != nil {
		t.Fatal(err)
	}
	wantAttachment := []byte{0, 255, 1, 254, 2, 0, 128, 42}
	if string(attachment) != string(wantAttachment) {
		t.Fatalf("attachment bytes changed: got %v want %v", attachment, wantAttachment)
	}
	var category string
	if err := f.sql.QueryRow(f.ctx, `SELECT i.category FROM issues i JOIN events e ON e.issue_id=i.id WHERE e.project_id=$1::bigint AND e.event_id=$2`, f.project.ID, feedbackID).Scan(&category); err != nil || category != "feedback" {
		t.Fatalf("feedback did not create its category issue: %q, %v", category, err)
	}

	// A common event item carries the same explicit feedback payload type.
	storePath := strings.TrimSuffix(path, "envelope/") + "store/"
	commonID := "b1000000000000000000000000000002"
	common := `{"event_id":"b1000000000000000000000000000002"}` + "\n" + `{"type":"event"}` + "\n" + `{"type":"feedback","event_id":"b1000000000000000000000000000002","contexts":{"feedback":{"message":"Common event feedback"}}}`
	decodeAck(f.request(t, http.DefaultClient, "POST", path, common, header, 200), commonID)
	var count int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE project_id=$1::bigint AND event_id=$2 AND kind='feedback'`, f.project.ID, commonID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("common event feedback was not stored: %d, %v", count, err)
	}
	const storeID = "b1000000000000000000000000000012"
	storeBody := `{"type":"feedback","event_id":"` + storeID + `","contexts":{"feedback":{"message":"Legacy store transport, modern feedback"}}}`
	decodeAck(f.request(t, http.DefaultClient, "POST", storePath, storeBody, header, 200), storeID)
	const defaultID = "b1000000000000000000000000000013"
	defaultBody := `{"event_id":"` + defaultID + `"}` + "\n" + `{"type":"feedback"}` + "\n" + `{"event_id":"` + defaultID + `","contexts":{"feedback":{"message":"Item supplies feedback type"}}}`
	decodeAck(f.request(t, http.DefaultClient, "POST", path, defaultBody, header, 200), defaultID)
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM events WHERE project_id=$1::bigint AND event_id=ANY($2::text[]) AND kind='feedback'`, f.project.ID, []string{storeID, defaultID}).Scan(&count); err != nil || count != 2 {
		t.Fatal("store/default-type feedback missing", count, err)
	}

	// Parent admission is shared across the three parent-bearing item forms.
	for name, second := range map[string]string{
		"feedback":    `{"type":"feedback"}` + "\n" + `{"type":"feedback","event_id":"b1000000000000000000000000000003","contexts":{"feedback":{"message":"one"}}}`,
		"event":       `{"type":"event"}` + "\n" + `{"event_id":"b1000000000000000000000000000003","message":"second"}`,
		"transaction": `{"type":"transaction"}` + "\n" + `{"event_id":"b1000000000000000000000000000003","transaction":"GET /","start_timestamp":1,"timestamp":2}`,
	} {
		t.Run("parent-limit/"+name, func(t *testing.T) {
			firstID := "b1000000000000000000000000000003"
			first := `{"type":"feedback"}` + "\n" + `{"type":"feedback","event_id":"b1000000000000000000000000000003","contexts":{"feedback":{"message":"one"}}}`
			bad := `{"event_id":"b1000000000000000000000000000003"}` + "\n" + first + "\n" + second
			before := captureSnapshot(t, f)
			f.request(t, http.DefaultClient, "POST", path, bad, header, 400)
			if after := captureSnapshot(t, f); after != before {
				t.Fatal("rejected multiple parents changed stored rows", firstID)
			}
		})
	}

	before := captureSnapshot(t, f)
	for label, bad := range map[string]string{
		"item payload contradiction":         `{"event_id":"b1000000000000000000000000000004"}` + "\n" + `{"type":"feedback"}` + "\n" + `{"type":"error","event_id":"b1000000000000000000000000000004","contexts":{"feedback":{"message":"bad"}}}`,
		"null payload type":                  `{"event_id":"b1000000000000000000000000000004"}` + "\n" + `{"type":"feedback"}` + "\n" + `{"type":null,"event_id":"b1000000000000000000000000000004","contexts":{"feedback":{"message":"bad"}}}`,
		"envelope payload identity mismatch": `{"event_id":"b1000000000000000000000000000005"}` + "\n" + `{"type":"feedback"}` + "\n" + `{"type":"feedback","event_id":"b1000000000000000000000000000006","contexts":{"feedback":{"message":"bad"}}}`,
		"legacy report on feedback":          `{"event_id":"b1000000000000000000000000000007"}` + "\n" + `{"type":"feedback"}` + "\n" + `{"type":"feedback","event_id":"b1000000000000000000000000000007","contexts":{"feedback":{"message":"ok"}}}` + "\n" + `{"type":"user_report"}` + "\n" + `{"event_id":"b1000000000000000000000000000007","name":"","email":"","comments":"legacy"}`,
	} {
		t.Run(label, func(t *testing.T) {
			f.request(t, http.DefaultClient, "POST", path, bad, header, 400)
			if after := captureSnapshot(t, f); after != before {
				t.Fatalf("invalid envelope changed persisted tables\nbefore %s\nafter  %s", before, after)
			}
		})
	}
	invalidKey := http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=wrong"}}
	f.request(t, http.DefaultClient, "POST", path, wire, invalidKey, 403)

	// Both directions of a feedback/error kind collision must reject bytes.
	const collisionID = "b1000000000000000000000000000008"
	errorBody := `{"event_id":"b1000000000000000000000000000008","message":"an error"}`
	f.request(t, http.DefaultClient, "POST", path, `{"event_id":"`+collisionID+`"}`+"\n"+`{"type":"event"}`+"\n"+errorBody, header, 200)
	feedbackCollision := strings.ReplaceAll(wire, feedbackID, collisionID)
	collisionBefore := captureSnapshot(t, f)
	f.request(t, http.DefaultClient, "POST", path, feedbackCollision, header, 409)
	if after := captureSnapshot(t, f); after != collisionBefore {
		t.Fatal("feedback collision changed the error receipt or children")
	}
	const feedbackFirstID = "b1000000000000000000000000000009"
	feedbackFirst := strings.ReplaceAll(wire, feedbackID, feedbackFirstID)
	f.request(t, http.DefaultClient, "POST", path, feedbackFirst, header, 200)
	collisionBefore = captureSnapshot(t, f)
	f.request(t, http.DefaultClient, "POST", path, `{"event_id":"`+feedbackFirstID+`"}`+"\n"+`{"type":"event"}`+"\n"+`{"event_id":"`+feedbackFirstID+`","message":"an error"}`, header, 409)
	if after := captureSnapshot(t, f); after != collisionBefore {
		t.Fatal("error collision changed the feedback receipt or children")
	}

	// A duplicate keeps the first payload and receipt clock while allowing a new child.
	const retryID = "b1000000000000000000000000000010"
	firstRetry := strings.ReplaceAll(strings.ReplaceAll(wire, feedbackID, retryID), "Associated feedback", "First feedback")
	f.request(t, http.DefaultClient, "POST", path, firstRetry, header, 200)
	var firstRaw string
	var firstDate time.Time
	if err := f.sql.QueryRow(f.ctx, `SELECT data::text,created_at FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, retryID).Scan(&firstRaw, &firstDate); err != nil {
		t.Fatal(err)
	}
	retry := strings.ReplaceAll(strings.ReplaceAll(wire, feedbackID, retryID), "Associated feedback", "Second feedback")
	retry = strings.Replace(retry, "feedback-proof.bin", "retry-child.bin", 1)
	f.request(t, http.DefaultClient, "POST", path, retry, header, 200)
	var retryRaw string
	var retryDate time.Time
	if err := f.sql.QueryRow(f.ctx, `SELECT data::text,created_at FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, retryID).Scan(&retryRaw, &retryDate); err != nil || retryRaw != firstRaw || !retryDate.Equal(firstDate) {
		t.Fatalf("feedback retry replaced first receipt: raw equal=%v date equal=%v err=%v", retryRaw == firstRaw, retryDate.Equal(firstDate), err)
	}
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM attachments WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, retryID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("feedback retry did not add its child attachment: %d, %v", count, err)
	}

	// A child SQL constraint fires after the parent/association writes. The
	// real HTTP transaction must leave these captured rows unchanged.
	if _, err := f.sql.Exec(f.ctx, `ALTER TABLE attachments ADD CONSTRAINT reject_feedback_late CHECK(filename<>'late-feedback.bin')`); err != nil {
		t.Fatal(err)
	}
	lateID := "b1000000000000000000000000000011"
	late := strings.ReplaceAll(wire, feedbackID, lateID)
	late = strings.Replace(late, "feedback-proof.bin", "late-feedback.bin", 1)
	lateBefore := captureSnapshot(t, f)
	f.request(t, http.DefaultClient, "POST", path, late, header, 503)
	if after := captureSnapshot(t, f); after != lateBefore {
		t.Fatalf("late SQL failure committed part of capture\nbefore %s\nafter  %s", lateBefore, after)
	}
}

func captureSnapshot(t *testing.T, f *resolutionHTTPFixture) string {
	t.Helper()
	var snapshot string
	err := f.sql.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM events e),
 'issues',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM issues i),
 'attachments',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM attachments a),
 'feedback',(SELECT jsonb_agg(to_jsonb(fe) ORDER BY fe.event_receipt_id) FROM feedback_events fe),
 'reports',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM user_reports r),
 'opaque',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM envelope_items o))::text`).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
