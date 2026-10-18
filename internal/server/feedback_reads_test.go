//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"reflect"
	"strings"
	"testing"
	"time"
)

// These tests own the native HTTP contract: strict transport admission,
// session-only visibility, no-store responses and the serialized view of real
// SDK captures. Normalization and association state generation have store owners.
func TestModernFeedbackReadHTTPContract(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	errorID := fmt.Sprintf("%032x", uint64(0xd123456789abcdef))
	feedbackID := fmt.Sprintf("%032x", uint64(0xe123456789abcdef))
	sdkHeaders := http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=" + f.project.PublicKey}}
	envelope := func(payload map[string]any) string {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return "{}\n{\"type\":\"event\"}\n" + string(body)
	}
	errorPayload := map[string]any{"event_id": errorID, "message": "Linked SDK error <script>raw</script>", "exception": map[string]any{"values": []any{map[string]any{"type": "SDKError", "value": "actual linked error"}}}}
	errorRaw, err := json.Marshal(errorPayload)
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/", envelope(errorPayload), sdkHeaders, 200)
	feedbackPayload := func(id, message string, associated string) string {
		fb := map[string]any{"message": message, "name": "Hostile <img src=x onerror=alert(1)>", "contact_email": "<script>sender</script>", "url": "https://example.invalid/?q=<script>", "associated_event_id": associated}
		return "{}\n{\"type\":\"feedback\"}\n" + fmt.Sprintf("{\"event_id\":%q,\"type\":\"feedback\",\"contexts\":{\"feedback\":%s}}", id, mustJSON(t, fb))
	}
	f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/", feedbackPayload(feedbackID, "Actual SDK feedback <script>alert(1)</script>", errorID), sdkHeaders, 200)

	var ownerIssue string
	if err := f.sql.QueryRow(f.ctx, `SELECT issue_id::text FROM events WHERE project_id=$1 AND event_id=$2`, f.project.ID, feedbackID).Scan(&ownerIssue); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/issues/" + ownerIssue + "/feedback"
	// The first read assertion is intentionally the route contract. On the
	// pre-reader baseline this must fail as a missing route, before auth/parser
	// assertions can produce an unrelated RED result.
	var page struct {
		Data []struct {
			EventID                   string
			DateCreated               time.Time
			Message, Name, Email, URL string
			AssociatedEventID         *string
			AssociationState          string
			AssociatedIssueID         *string
		}
		Total          int64
		Page, PageSize int
		HasMore        bool
	}
	decode := func(raw []byte, dst any) {
		t.Helper()
		if err := json.Unmarshal(raw, dst); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	resp, err := f.client.Get(f.server.URL + "/good" + base)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readResponse(resp)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("feedback page status=%d err=%v body=%s", resp.StatusCode, err, raw)
	}
	cacheControl := strings.ToLower(resp.Header.Get("Cache-Control"))
	if !strings.Contains(cacheControl, "private") || !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("missing private no-store: %v", resp.Header)
	}
	decode(raw, &page)
	if len(page.Data) != 1 || page.Total != 1 || page.Page != 1 || page.PageSize != 25 || page.HasMore || page.Data[0].EventID != feedbackID || page.Data[0].Message != "Actual SDK feedback <script>alert(1)</script>" || page.Data[0].Name != "Hostile <img src=x onerror=alert(1)>" || page.Data[0].Email != "<script>sender</script>" || page.Data[0].AssociatedEventID == nil || *page.Data[0].AssociatedEventID != errorID || page.Data[0].AssociationState != "linked" || page.Data[0].AssociatedIssueID == nil || page.Data[0].DateCreated.IsZero() || page.Data[0].DateCreated.Location() != time.UTC {
		t.Fatalf("reader did not return current scoped SDK receipt: %+v", page)
	}
	issueList := "/api/v1/organizations/" + f.org.Slug + "/issues?status=all"
	var filtered struct {
		Data  []struct{ ID, Category string }
		Total int
	}
	decode(f.request(t, f.client, "GET", issueList+"&category=feedback", "", nil, 200), &filtered)
	if filtered.Total != 1 || len(filtered.Data) != 1 || filtered.Data[0].ID != ownerIssue || filtered.Data[0].Category != "feedback" {
		t.Fatal("native category query is disconnected", filtered)
	}
	decode(f.request(t, f.client, "GET", issueList+"&category=error", "", nil, 200), &filtered)
	if filtered.Total == 0 {
		t.Fatal("error category lost actual errors")
	}
	for _, row := range filtered.Data {
		if row.Category != "error" || row.ID == ownerIssue {
			t.Fatal("error totals include feedback", filtered)
		}
	}
	for _, suffix := range []string{"&category=all", "&category=feedback&category=error", "&query=issue.category%3Aother"} {
		f.request(t, f.client, "GET", issueList+suffix, "", nil, 400)
	}
	var compatibleRows []struct{ ID, IssueCategory, Type string }
	decode(f.request(t, f.client, "GET", "/api/0/organizations/"+f.org.Slug+"/issues/?query=issue.category%3Afeedback", "", nil, 200), &compatibleRows)
	if len(compatibleRows) != 1 || compatibleRows[0].ID != ownerIssue || compatibleRows[0].IssueCategory != "feedback" || compatibleRows[0].Type != "feedback" {
		t.Fatal("compatible category query/projection disconnected", compatibleRows)
	}
	var projectedEvent struct {
		Type, Message string
		UserReport    *json.RawMessage
	}
	decode(f.request(t, f.client, "GET", "/api/0/issues/"+ownerIssue+"/events/"+feedbackID+"/", "", nil, 200), &projectedEvent)
	if projectedEvent.Type != "feedback" || projectedEvent.Message != page.Data[0].Message || projectedEvent.UserReport != nil {
		t.Fatal("compatible event lost feedback projection", projectedEvent)
	}
	var emptyPage struct {
		Data           []json.RawMessage
		Total          int64
		Page, PageSize int
		HasMore        bool
	}
	decode(f.request(t, f.client, "GET", base+"?page=2", "", nil, 200), &emptyPage)
	if emptyPage.Data == nil || len(emptyPage.Data) != 0 || emptyPage.Total != 1 || emptyPage.Page != 2 || emptyPage.PageSize != 25 || emptyPage.HasMore {
		t.Fatalf("empty feedback page shape: %+v", emptyPage)
	}
	// Ordinary organization membership is sufficient for this session read.
	var memberID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,name,password_hash) VALUES('feedback-member@resolution.test','Feedback member',$1) RETURNING id::text`, f.hash).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships VALUES($1,$2,'member')`, f.org.ID, memberID); err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	memberClient := &http.Client{Jar: jar}
	var memberLogin struct{ CSRFToken string }
	if err := json.Unmarshal(f.request(t, memberClient, "POST", "/api/v1/login", `{"email":"feedback-member@resolution.test","password":"resolution-http-password"}`, nil, 200), &memberLogin); err != nil {
		t.Fatal(err)
	}
	f.request(t, memberClient, "GET", base, "", nil, 200)
	for _, p := range []string{base, base + "/" + feedbackID + "/associated-event"} {
		f.request(t, http.DefaultClient, "GET", p, "", http.Header{"Authorization": {"Bearer gs_invalid"}}, 401)
	}

	var associated struct {
		EventID string
		IssueID string
		Event   json.RawMessage
	}
	decode(f.request(t, f.client, "GET", base+"/"+feedbackID+"/associated-event", "", nil, 200), &associated)
	var rawEvent map[string]any
	decode(associated.Event, &rawEvent)
	if associated.EventID != errorID || associated.IssueID == "" || rawEvent["message"] != "Linked SDK error <script>raw</script>" || !sameFeedbackJSON(associated.Event, errorRaw) {
		t.Fatalf("associated reader did not return exact linked SDK event: %+v", associated)
	}
	if _, err := f.sql.Exec(f.ctx, `UPDATE events SET created_at=clock_timestamp()-interval '2 days' WHERE project_id=$1 AND event_id=$2`, f.project.ID, errorID); err != nil {
		t.Fatal(err)
	}
	if n, err := f.db.CleanupEvents(f.ctx, 1); err != nil || n != 1 {
		t.Fatalf("expire linked error receipt count=%d err=%v", n, err)
	}
	f.request(t, f.client, "GET", base+"/"+feedbackID+"/associated-event", "", nil, 404)
	f.request(t, f.client, "GET", base+"/"+feedbackID+"/associated-event?x=1", "", nil, 400)
	for _, q := range []string{"page=unknown", "page=", "page=01", "page=1&page=1", "page=%ZZ"} {
		f.request(t, f.client, "GET", base+"?"+q, "", nil, 400)
	}
	f.request(t, f.client, "GET", "/api/v1/issues/not-an-id/feedback", "", nil, 404)
	f.request(t, f.client, "GET", base+"/not-a-feedback-id/associated-event", "", nil, 400)
	// A syntactically valid feedback reference with no retained target must not
	// be resolved by SDK ID or substituted with the latest issue event.
	unboundID := fmt.Sprintf("%032x", uint64(0xa123456789abcdef))
	missingTargetID := fmt.Sprintf("%032x", uint64(0xb123456789abcdef))
	f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/", feedbackPayload(unboundID, "unbound SDK feedback", missingTargetID), sdkHeaders, 200)
	var unboundIssue string
	if err := f.sql.QueryRow(f.ctx, `SELECT issue_id::text FROM events WHERE project_id=$1 AND event_id=$2`, f.project.ID, unboundID).Scan(&unboundIssue); err != nil {
		t.Fatal(err)
	}
	f.request(t, f.client, "GET", "/api/v1/issues/"+unboundIssue+"/feedback/"+unboundID+"/associated-event", "", nil, 404)
	f.request(t, f.client, "GET", "/api/v1/issues/999999999/feedback", "", nil, 404)
	f.request(t, f.client, "GET", base+"/ffffffffffffffffffffffffffffffff/associated-event", "", nil, 404)

	// Generate every row through SDK envelope admission, then use the real
	// store merge operation to make a legitimate 27-receipt pagination cohort.
	var issueIDs []string
	for n := 0; n < 27; n++ {
		id := fmt.Sprintf("%032x", 0x1000+n)
		f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/", feedbackPayload(id, fmt.Sprintf("page receipt %02d", n), ""), sdkHeaders, 200)
		var issueID string
		if err := f.sql.QueryRow(f.ctx, `SELECT issue_id::text FROM events WHERE project_id=$1 AND event_id=$2`, f.project.ID, id).Scan(&issueID); err != nil {
			t.Fatal(err)
		}
		issueIDs = append(issueIDs, issueID)
	}
	merged, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, issueIDs)
	if err != nil {
		t.Fatal(err)
	}
	var mergedPage struct {
		Data           []json.RawMessage
		Total          int64
		Page, PageSize int
		HasMore        bool
	}
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+merged.Issue.ID+"/feedback?page=1", "", nil, 200), &mergedPage)
	if len(mergedPage.Data) != 25 || mergedPage.Total != 27 || mergedPage.Page != 1 || mergedPage.PageSize != 25 || !mergedPage.HasMore {
		t.Fatalf("first feedback page metadata: %+v", mergedPage)
	}
	var next struct {
		Data     []json.RawMessage
		Total    int64
		Page     int
		PageSize int
		HasMore  bool
	}
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+merged.Issue.ID+"/feedback?page=2", "", nil, 200), &next)
	if len(next.Data) != 2 || next.Total != 27 || next.Page != 2 || next.PageSize != 25 || next.HasMore {
		t.Fatalf("second feedback page metadata: %+v", next)
	}
	var pastEnd struct {
		Data           []json.RawMessage
		Total          int64
		Page, PageSize int
		HasMore        bool
	}
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+merged.Issue.ID+"/feedback?page=3", "", nil, 200), &pastEnd)
	if pastEnd.Data == nil || len(pastEnd.Data) != 0 || pastEnd.Total != 27 || pastEnd.Page != 3 || pastEnd.PageSize != 25 || pastEnd.HasMore {
		t.Fatalf("past-end feedback page: %+v", pastEnd)
	}
	// An older merged alias resolves to the same visible current root.
	var aliasPage struct {
		Data           []json.RawMessage
		Total          int64
		Page, PageSize int
		HasMore        bool
	}
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+issueIDs[0]+"/feedback?page=2", "", nil, 200), &aliasPage)
	if len(aliasPage.Data) != 2 || aliasPage.Total != 27 {
		t.Fatalf("feedback alias did not resolve to its current owner: %+v", aliasPage)
	}
	f.request(t, f.client, "GET", "/api/v1/issues/"+merged.Issue.ID+"/feedback/"+feedbackID+"/associated-event", "", nil, 404)
	var detail struct {
		FeedbackItems []json.RawMessage
		FeedbackCount int64
	}
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+merged.Issue.ID, "", nil, 200), &detail)
	if len(detail.FeedbackItems) != 25 || detail.FeedbackCount != 27 {
		t.Fatalf("native detail feedback projection: %+v", detail)
	}
	zeroID := fmt.Sprintf("%032x", uint64(0xc123456789abcdef))
	f.request(t, http.DefaultClient, "POST", "/api/"+f.project.ID+"/envelope/", feedbackPayload(zeroID, "retained zero-event feedback", ""), sdkHeaders, 200)
	var zeroIssue string
	if err := f.sql.QueryRow(f.ctx, `SELECT issue_id::text FROM events WHERE project_id=$1 AND event_id=$2`, f.project.ID, zeroID).Scan(&zeroIssue); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `UPDATE events SET created_at=clock_timestamp()-interval '2 days' WHERE project_id=$1 AND event_id=$2`, f.project.ID, zeroID); err != nil {
		t.Fatal(err)
	}
	if n, err := f.db.CleanupEvents(f.ctx, 1); err != nil || n != 1 {
		t.Fatalf("expire feedback receipt count=%d err=%v", n, err)
	}
	var zeroDetail struct {
		Issue         struct{ Category, Status string }
		FeedbackItems []json.RawMessage
		FeedbackCount int64
	}
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+zeroIssue, "", nil, 200), &zeroDetail)
	var zeroPage struct {
		Data           []json.RawMessage
		Total          int64
		Page, PageSize int
		HasMore        bool
	}
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+zeroIssue+"/feedback", "", nil, 200), &zeroPage)
	if zeroDetail.Issue.Category != "feedback" || zeroDetail.Issue.Status != "unresolved" || zeroDetail.FeedbackItems == nil || len(zeroDetail.FeedbackItems) != 0 || zeroDetail.FeedbackCount != 0 || zeroPage.Data == nil || len(zeroPage.Data) != 0 || zeroPage.Total != 0 {
		t.Fatalf("zero-event feedback issue was lost or fabricated: detail=%+v page=%+v", zeroDetail, zeroPage)
	}
	// Category-only compatible bulk selection must mutate feedback issues while
	// leaving the fixture's unresolved error issue untouched. Deletion then uses
	// the newly resolved current status as part of the category selection.
	orgIssues := "/api/0/organizations/" + f.org.Slug + "/issues/"
	csrf := http.Header{"X-CSRF-Token": {f.csrf}}
	f.request(t, f.client, "PUT", orgIssues+"?query=issue.category%3Afeedback", `{"status":"resolved"}`, csrf, 200)
	var feedbackState, errorState struct{ Issue struct{ Status string } }
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+merged.Issue.ID, "", nil, 200), &feedbackState)
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+f.issueID, "", nil, 200), &errorState)
	decode(f.request(t, f.client, "GET", "/api/v1/issues/"+zeroIssue, "", nil, 200), &zeroDetail)
	if feedbackState.Issue.Status != "resolved" || zeroDetail.Issue.Status != "resolved" || errorState.Issue.Status != "unresolved" {
		t.Fatalf("category-only PUT selected wrong issue states: feedback=%s error=%s", feedbackState.Issue.Status, errorState.Issue.Status)
	}
	f.request(t, f.client, "DELETE", orgIssues+"?query=issue.category%3Afeedback&status=resolved", "", csrf, 200)
	f.request(t, f.client, "GET", "/api/v1/issues/"+merged.Issue.ID, "", nil, 404)
	f.request(t, f.client, "GET", "/api/v1/issues/"+zeroIssue, "", nil, 404)
	f.request(t, f.client, "GET", "/api/v1/issues/"+f.issueID, "", nil, 200)

	// A separate, real owner/org/project captures feedback through the SDK path.
	var foreignOwnerID, foreignOrgID string
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO users(email,name,password_hash) VALUES('foreign-feedback@resolution.test','Foreign owner',$1) RETURNING id::text`, f.hash).Scan(&foreignOwnerID); err != nil {
		t.Fatal(err)
	}
	if err := f.sql.QueryRow(f.ctx, `INSERT INTO organizations(slug,name) VALUES('foreign-feedback-reader','Foreign feedback') RETURNING id::text`).Scan(&foreignOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(f.ctx, `INSERT INTO memberships VALUES($1,$2,'owner')`, foreignOrgID, foreignOwnerID); err != nil {
		t.Fatal(err)
	}
	var foreignOrgSlug string
	if err := f.sql.QueryRow(f.ctx, `SELECT slug FROM organizations WHERE id=$1`, foreignOrgID).Scan(&foreignOrgSlug); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := f.db.CreateProject(f.ctx, foreignOwnerID, foreignOrgSlug, "Foreign feedback project", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	foreignHeaders := http.Header{"X-Sentry-Auth": {"Sentry sentry_version=7,sentry_key=" + foreignProject.PublicKey}}
	foreignFeedbackID := fmt.Sprintf("%032x", uint64(0xf123456789abcdef))
	f.request(t, http.DefaultClient, "POST", "/api/"+foreignProject.ID+"/envelope/", feedbackPayload(foreignFeedbackID, "foreign actual feedback", ""), foreignHeaders, 200)
	var foreignIssue string
	if err := f.sql.QueryRow(f.ctx, `SELECT issue_id::text FROM events WHERE project_id=$1 AND event_id=$2`, foreignProject.ID, foreignFeedbackID).Scan(&foreignIssue); err != nil {
		t.Fatal(err)
	}
	f.request(t, f.client, "GET", "/api/v1/issues/"+foreignIssue+"/feedback", "", nil, 404)
	f.request(t, f.client, "GET", "/api/v1/issues/"+foreignIssue+"/feedback/"+feedbackID+"/associated-event", "", nil, 404)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func sameFeedbackJSON(left, right []byte) bool {
	decode := func(raw []byte) (any, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		err := decoder.Decode(&value)
		return value, err
	}
	a, err := decode(left)
	if err != nil {
		return false
	}
	b, err := decode(right)
	return err == nil && reflect.DeepEqual(a, b)
}
