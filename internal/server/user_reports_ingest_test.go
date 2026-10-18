//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

// Actual HTTP owns recognizing the public SDK item rather than opaque storage,
// identity/field prevalidation and atomic parent/children dispatch. Storage tests
// cannot detect an unsupportedItems acknowledgment or a missed parser branch.
// Wire shape comes from an observed official Browser7.120.4 custom transport.
func TestUserReportSDKEnvelopeHTTP(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	path := "/api/" + f.project.ID + "/envelope/"
	headers := http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=" + f.project.PublicKey}}
	const eventID = "0123456789abcdef0123456789abcdef"
	const reportJSON = `{"event_id":"0123456789abcdef0123456789abcdef","name":"SDK Probe","email":"probe@example.invalid","comments":"legacy SDK feedback probe"}`
	// Exact public item grammar from the official legacy browser SDK; the only
	// fixture substitution is the authenticated local project key in the header.
	envelope := `{"event_id":"0123456789abcdef0123456789abcdef","sdk":{"name":"sentry.javascript.browser","version":"7.120.4"}}` + "\n" + `{"type":"user_report"}` + "\n" + reportJSON
	var ack struct {
		ID               string
		UnsupportedItems []string
	}
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "POST", path, envelope, headers, 200), &ack); err != nil || ack.ID != eventID || len(ack.UnsupportedItems) != 0 {
		t.Fatal("SDK feedback was accepted as an opaque unsupported item", ack, err)
	}
	projectPath := "/api/0/projects/" + f.org.Slug + "/" + f.project.Slug + "/user-feedback/"
	var reports []store.UserReport
	if err := json.Unmarshal(f.request(t, f.client, "GET", projectPath, "", nil, 200), &reports); err != nil || len(reports) != 1 || reports[0].Comments != "legacy SDK feedback probe" {
		t.Fatal("acknowledged SDK report is not readable", reports, err)
	}
	// Reverse arrival uses the real envelope endpoint and then the existing
	// native issue read, rather than manually assigning a report's event FK.
	errorEnvelope := `{"event_id":"0123456789abcdef0123456789abcdef"}` + "\n" + `{"type":"event"}` + "\n" + `{"event_id":"0123456789abcdef0123456789abcdef","message":"Resolution HTTP fixture"}`
	f.request(t, http.DefaultClient, "POST", path, errorEnvelope, headers, 200)
	var detail struct {
		Issue           struct{ Count int64 }
		UserReportCount int64
	}
	if err := json.Unmarshal(f.request(t, f.client, "GET", "/api/v1/issues/"+f.issueID, "", nil, 200), &detail); err != nil || detail.Issue.Count != 2 || detail.UserReportCount != 1 {
		t.Fatal("SDK reversed arrival failed or report inflated errors", detail, err)
	}
	// A binary attachment and legacy report are one capture transaction.
	const parentID = "cccccccccccccccccccccccccccccccc"
	parent := `{"event_id":"cccccccccccccccccccccccccccccccc","message":"Atomic SDK report error"}`
	child := `{"event_id":"cccccccccccccccccccccccccccccccc","name":"","email":"","comments":"Atomic report"}`
	combined := `{"event_id":"cccccccccccccccccccccccccccccccc"}` + "\n" + `{"type":"event"}` + "\n" + parent + "\n" + `{"type":"attachment","filename":"report.bin","length":3}` + "\n" + string([]byte{0, 255, 1}) + "\n" + `{"type":"user_report"}` + "\n" + child
	f.request(t, http.DefaultClient, "POST", path, combined, headers, 200)
	var rows int
	if err := f.sql.QueryRow(f.ctx, `SELECT count(*) FROM user_reports r JOIN events e ON e.id=r.event_receipt_id JOIN attachments a ON a.project_id=e.project_id AND a.event_id=e.event_id WHERE e.event_id=$1`, parentID).Scan(&rows); err != nil || rows != 1 {
		t.Fatal("combined SDK children not committed together", rows, err)
	}
	uuidReport := strings.Replace(reportJSON, eventID, "ABCDEF01-2345-6789-ABCD-EF0123456789", 1)
	if err := json.Unmarshal(f.request(t, http.DefaultClient, "POST", path, "{}\n"+`{"type":"user_report"}`+"\n"+uuidReport, headers, 200), &ack); err != nil || ack.ID != "abcdef0123456789abcdef0123456789" || len(ack.UnsupportedItems) != 0 {
		t.Fatal("report-only identity without envelope header was lost", ack, err)
	}
	snapshot := func() string {
		t.Helper()
		var rows string
		if err := f.sql.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM events e),
 'issues',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM issues i),
 'attachments',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM attachments a),
 'reports',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM user_reports r),
 'opaque',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM envelope_items o))::text`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := snapshot()
	for label, bad := range map[string]string{
		"wrong parent":          strings.Replace(combined, child, reportJSON, 1),
		"two reports":           combined + "\n" + `{"type":"user_report"}` + "\n" + child,
		"empty comments":        strings.Replace(combined, `"Atomic report"`, `"  "`, 1),
		"null name":             strings.Replace(combined, `"name":""`, `"name":null`, 1),
		"missing email":         strings.Replace(combined, `"email":"",`, "", 1),
		"wrong field type":      strings.Replace(combined, `"comments":"Atomic report"`, `"comments":3`, 1),
		"name limit":            strings.Replace(combined, `"name":""`, `"name":"`+strings.Repeat("界", 129)+`"`, 1),
		"email limit":           strings.Replace(combined, `"email":""`, `"email":"`+strings.Repeat("a", 255)+`"`, 1),
		"comments limit":        strings.Replace(combined, "Atomic report", strings.Repeat("x", 10001), 1),
		"invalid UTF8":          strings.Replace(combined, "Atomic report", string([]byte{255}), 1),
		"header mismatch":       strings.Replace(envelope, eventID, strings.Repeat("f", 32), 1),
		"transaction parent":    strings.Replace(combined, parent, `{"event_id":"cccccccccccccccccccccccccccccccc","type":"transaction","transaction":"GET /test","start_timestamp":1,"timestamp":2}`, 1),
		"standalone attachment": `{"event_id":"cccccccccccccccccccccccccccccccc"}` + "\n" + `{"type":"user_report"}` + "\n" + child + "\n" + `{"type":"attachment","length":1}` + "\n" + "x",
	} {
		t.Run(label, func(t *testing.T) {
			// Every combined rejection must protect a fresh parent identity,
			// not merely a harmless retry of the already stored capture.
			bad = strings.ReplaceAll(bad, parentID, strings.Repeat("d", 32))
			f.request(t, http.DefaultClient, "POST", path, bad, headers, 400)
			if after := snapshot(); after != before {
				t.Fatal("invalid envelope changed stored rows", before, after)
			}
		})
	}
	// A late database rejection proves that the HTTP dispatcher actually put
	// the report in CaptureItems, rather than saving it after parent commit.
	if _, err := f.sql.Exec(f.ctx, `ALTER TABLE user_reports ADD CONSTRAINT reject_report_http CHECK(comments<>'Late report failure')`); err != nil {
		t.Fatal(err)
	}
	late := strings.ReplaceAll(strings.Replace(combined, "Atomic report", "Late report failure", 1), parentID, strings.Repeat("e", 32))
	f.request(t, http.DefaultClient, "POST", path, late, headers, 503)
	if after := snapshot(); after != before {
		t.Fatal("HTTP committed a parent before its report failed", before, after)
	}
	if _, err := f.sql.Exec(f.ctx, `UPDATE user_reports SET created_at=clock_timestamp()-interval '6 minutes' WHERE event_id=$1`, parentID); err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "POST", path, combined, headers, 409)
	f.request(t, http.DefaultClient, "POST", path, envelope, http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=wrong"}}, 403)
}
