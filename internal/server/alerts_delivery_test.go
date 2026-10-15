//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodsentry/internal/jobs"
	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

// This local-only acceptance owns capture/check -> persisted outbox -> real
// recipient. Production startup does not enable outbound delivery. The test
// owns its isolated database schema and sole httptest recipient; it never
// consumes configured application queues or contacts real email/webhook users.
func verifyLocalAlertDelivery(t *testing.T, request func(string, string, string, string) (int, map[string]any), db *store.Store, base, org, project, projectSlug, csrf, ingestion string) {
	t.Helper()
	var mu sync.Mutex
	type receipt struct{ ID, HeaderID, Kind, URL string }
	var receipts []receipt
	var permanent atomic.Bool
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload notify.Payload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		receipts = append(receipts, receipt{payload.ID, r.Header.Get("X-GoodSentry-Delivery-ID"), payload.Kind, payload.URL})
		n := len(receipts)
		mu.Unlock()
		if permanent.Load() {
			w.WriteHeader(600)
			return
		}
		if n == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	var console bytes.Buffer
	sender, err := notify.NewSender(notify.Options{Domain: "http://example.test", EmailURL: "consolemail://", FromEmail: "fixture@example.test", Timeout: time.Second, AllowPrivate: true, Console: &console})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	stopAlerts := jobs.StartAlerts(ctx, db, sender, jobs.AlertOptions{Workers: 2, MaxAttempts: 3, Timeout: time.Second})
	defer stopAlerts()
	collection := "/api/v1/organizations/" + org + "/projects/" + project + "/alerts"
	deliveries := strings.TrimSuffix(collection, "/alerts") + "/alert-deliveries"
	create := func(kind, environment string) string {
		t.Helper()
		body := fmt.Sprintf(`{"name":"Local-only delivery proof","kind":%q,"quantity":1,"environments":[%q],"recipients":[{"type":"webhook","destination":%q}]}`, kind, environment, receiver.URL)
		code, rule := request("POST", collection, body, csrf)
		if code != 201 {
			t.Fatalf("local rule=%d %+v", code, rule)
		}
		return rule["id"].(string)
	}
	waitDeliveries := func(rule, kind string, wantSent int) []any {
		t.Helper()
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			code, list := request("GET", deliveries+"?rule="+rule, "", "")
			if code != 200 {
				t.Fatalf("delivery read=%d %+v", code, list)
			}
			items := list["data"].([]any)
			sent, matching := 0, false
			for _, item := range items {
				d := item.(map[string]any)
				if d["status"] == "sent" {
					sent++
					matching = matching || d["payload"].(map[string]any)["kind"] == kind
				}
			}
			if sent == wantSent && matching {
				return items
			}
			time.Sleep(40 * time.Millisecond)
		}
		t.Fatalf("local worker did not deliver %s", kind)
		return nil
	}
	rule := create("error_frequency", "local-alert-proof")
	res, err := http.Post(ingestion, "text/plain", strings.NewReader("{}\n{\"type\":\"event\"}\n"+`{"event_id":"00000000000000000000000000000c01","message":"Local delivery proof","environment":"local-alert-proof"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("local capture=%d", res.StatusCode)
	}
	items := waitDeliveries(rule, "error_frequency", 1)
	delivery := items[0].(map[string]any)
	if delivery["attempts"] != float64(2) {
		t.Fatalf("transient response was not retried: %+v", delivery)
	}
	code, detail := request("GET", deliveries+"/"+delivery["id"].(string), "", "")
	if code != 200 || len(detail["attempts"].([]any)) != 2 {
		t.Fatalf("local attempt history=%d %+v", code, detail)
	}
	mu.Lock()
	got := append([]receipt(nil), receipts...)
	mu.Unlock()
	if len(got) != 2 || got[0].ID != got[1].ID || got[0].ID != got[0].HeaderID || got[1].ID != got[1].HeaderID || !strings.HasPrefix(got[1].URL, "http://example.test/?") {
		t.Fatalf("retry identity/resource links=%+v", got)
	}
	uptimeRule := create("uptime", "local-uptime-proof")
	code, monitor := request("POST", "/api/v1/organizations/"+org+"/monitors", fmt.Sprintf(`{"name":"Local heartbeat delivery","type":"heartbeat","project":%q,"environment":"local-uptime-proof","intervalSeconds":1}`, projectSlug), csrf)
	if code != 201 || monitor["environment"] != "local-uptime-proof" {
		t.Fatalf("monitor environment=%d %+v", code, monitor)
	}
	beat := func() {
		t.Helper()
		endpoint := base + strings.TrimPrefix(monitor["heartbeatURL"].(string), "http://example.test")
		res, err := http.Post(endpoint, "text/plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("local heartbeat=%d", res.StatusCode)
		}
	}
	beat()
	stopMonitors := jobs.StartMonitors(ctx, db, 1, true)
	defer stopMonitors()
	waitDeliveries(uptimeRule, "uptime_down", 1)
	beat()
	waitDeliveries(uptimeRule, "uptime_recovered", 2)
	stopMonitors()
	// A parsed but unregistered three-digit reply is a permanent failure. It
	// must remain recordable rather than breaking the outbox SQL constraint.
	permanent.Store(true)
	permanentRule := create("error_frequency", "local-permanent-proof")
	res, err = http.Post(ingestion, "text/plain", strings.NewReader("{}\n{\"type\":\"event\"}\n"+`{"event_id":"00000000000000000000000000000c02","message":"Permanent local response","environment":"local-permanent-proof"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("permanent-response capture=%d", res.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, list := request("GET", deliveries+"?rule="+permanentRule, "", "")
		if code != 200 {
			t.Fatalf("permanent-response history=%d %+v", code, list)
		}
		for _, item := range list["data"].([]any) {
			d := item.(map[string]any)
			if d["status"] == "failed" && d["lastStatusCode"] == float64(600) && d["attempts"] == float64(1) {
				return
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("permanent HTTP 600 could not be persisted as a failed delivery")
}
