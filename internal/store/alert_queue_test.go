//go:build integration

package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/notify"
)

func TestAlertClaimsRetryBackoffAndTokenFencing(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("Retry queue"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	leases, errs := make(chan notify.Lease, 8), make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			lease, err := db.ClaimAlertDelivery(ctx, 20, time.Second)
			leases <- lease
			errs <- err
		})
	}
	wg.Wait()
	close(leases)
	close(errs)
	winners, missing := 0, 0
	var first notify.Lease
	for lease := range leases {
		if lease.Token != "" {
			first, winners = lease, winners+1
		}
	}
	for err := range errs {
		if errors.Is(err, ErrNotFound) {
			missing++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if winners != 1 || missing != 7 || first.Attempts != 1 {
		t.Fatalf("queue claim winners=%d missing=%d lease=%+v", winners, missing, first)
	}
	lease := first
	for attempt, seconds := range []int{15, 30, 60, 120, 240, 480, 900, 900} {
		if attempt != 0 {
			if _, err := db.pool.Exec(ctx, "UPDATE alert_deliveries SET next_attempt=clock_timestamp()-interval '1 second' WHERE id=$1", first.ID); err != nil {
				t.Fatal(err)
			}
			lease, err = db.ClaimAlertDelivery(ctx, 20, time.Second)
			if err != nil || lease.Attempts != attempt+1 {
				t.Fatalf("retry claim %d: %+v, %v", attempt, lease, err)
			}
			if err := db.CompleteAlertDelivery(ctx, first, notify.Outcome{Disposition: notify.Delivered, Reason: "ok"}, 20); !errors.Is(err, ErrConflict) {
				t.Fatalf("old token completed active replacement lease: %v", err)
			}
		}
		if err := db.CompleteAlertDelivery(ctx, lease, notify.Outcome{Disposition: notify.Retry, Reason: "connection_error", ElapsedMS: 12}, 20); err != nil {
			t.Fatal(err)
		}
		delivery, history, err := db.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, first.ID)
		if err != nil || delivery.Status != notify.Pending || delivery.NextAttempt.Sub(delivery.UpdatedAt) != time.Duration(seconds)*time.Second || len(history) != attempt+1 || history[attempt].CompletedAt == nil {
			t.Fatalf("retry delay/history %d: %+v history=%+v, %v", attempt, delivery, history, err)
		}
	}
}

func TestAlertExpiredClaimsRecoverOrStopAtAttemptBudget(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("Expired queue"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil {
		t.Fatal(err)
	}
	first, err := db.ClaimAlertDelivery(ctx, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE alert_deliveries SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	second, err := reopened.ClaimAlertDelivery(ctx, 2, time.Second)
	if err != nil || second.Token == first.Token || second.Attempts != 2 {
		t.Fatalf("expired claim recovery: %+v, %v", second, err)
	}
	if err := reopened.CompleteAlertDelivery(ctx, first, notify.Outcome{Disposition: notify.Delivered, Reason: "ok"}, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("restarted stale lease completed: %v", err)
	}
	if _, err := reopened.pool.Exec(ctx, "UPDATE alert_deliveries SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ClaimAlertDelivery(ctx, 2, time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("exhausted expired attempt reclaimed: %v", err)
	}
	delivery, attempts, err := reopened.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, second.ID)
	if err != nil || delivery.Status != notify.Failed || len(attempts) != 2 || attempts[0].Reason != "lease_expired" || attempts[1].Reason != "lease_expired" || attempts[1].CompletedAt == nil {
		t.Fatalf("abandoned attempt history: %+v attempts=%+v, %v", delivery, attempts, err)
	}
}

func TestAlertOutcomesPersistSuccessPermanentAndAttemptExhaustion(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("Outcomes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		disposition notify.Disposition
		want        notify.State
		code        int
	}{
		{notify.Retry, notify.Failed, 503}, {notify.Permanent, notify.Failed, 400}, {notify.Delivered, notify.Sent, 204},
	} {
		if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil {
			t.Fatal(err)
		}
		lease, err := db.ClaimAlertDelivery(ctx, 1, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		outcome := notify.Outcome{Disposition: tc.disposition, Reason: "http_status", StatusCode: &tc.code, ElapsedMS: 17}
		if err := db.CompleteAlertDelivery(ctx, lease, outcome, 1); err != nil {
			t.Fatal(err)
		}
		d, attempts, err := db.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, lease.ID)
		if err != nil || d.Status != tc.want || d.Attempts != 1 || d.LastStatusCode == nil || *d.LastStatusCode != tc.code || len(attempts) != 1 || attempts[0].ElapsedMS == nil || *attempts[0].ElapsedMS != 17 || attempts[0].Status != string(tc.disposition) || ((d.SentAt != nil) != (tc.want == notify.Sent)) {
			t.Fatalf("outcome persistence: %+v attempts=%+v, %v", d, attempts, err)
		}
	}
}

func TestAlertClaimBoundsExpiredBacklogCleanup(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("Restart backlog"))
	if err != nil {
		t.Fatal(err)
	}
	for range 129 {
		if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ClaimAlertDelivery(ctx, 1, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, "UPDATE alert_deliveries SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE project_id=$1::bigint", project.ID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ failed, processing int }{{128, 1}, {129, 0}} {
		if _, err := db.ClaimAlertDelivery(ctx, 1, time.Minute); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired exhausted backlog returned a lease: %v", err)
		}
		var failed, processing, complete int
		if err := db.pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE status='failed'),count(*) FILTER(WHERE status='processing') FROM alert_deliveries WHERE project_id=$1::bigint", project.ID).Scan(&failed, &processing); err != nil {
			t.Fatal(err)
		}
		if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM alert_attempts WHERE status='lease_expired' AND completed_at IS NOT NULL").Scan(&complete); err != nil {
			t.Fatal(err)
		}
		if failed != want.failed || processing != want.processing || complete != want.failed {
			t.Fatalf("cleanup did not commit a bounded batch: failed=%d processing=%d completedAttempts=%d want=%+v", failed, processing, complete, want)
		}
	}
}

func TestAlertReplacementCancelsWorkAndRetainsSnapshots(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	c := alertConfig("Original rule")
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, c)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil || n != 1 {
		t.Fatalf("queue test: %d, %v", n, err)
	}
	first, err := db.ClaimAlertDelivery(ctx, 3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteAlertDelivery(ctx, first, notify.Outcome{Disposition: notify.Delivered, Reason: "ok"}, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil {
		t.Fatal(err)
	}
	active, err := db.ClaimAlertDelivery(ctx, 3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Name, c.Recipients = "Changed rule", []notify.Recipient{{Type: "email", Destination: "changed@example.test"}}
	if _, err := db.UpdateAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID, c); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteAlertDelivery(ctx, active, notify.Outcome{Disposition: notify.Delivered, Reason: "ok"}, 3); !errors.Is(err, ErrConflict) {
		t.Fatalf("replaced-rule lease completed: %v", err)
	}
	snapshot, _, err := db.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, first.ID)
	if err != nil || snapshot.Status != notify.Sent || snapshot.RuleName != "Original rule" || snapshot.Recipient.Destination != "owner@example.test" || snapshot.Payload.Rule.Name != "Original rule" || snapshot.Payload.ID != snapshot.ID {
		t.Fatalf("completed delivery snapshot changed: %+v, %v", snapshot, err)
	}
	c.Enabled = false
	if _, err := db.UpdateAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("disabled test alert queued: %v", err)
	}
	if err := db.DeleteAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, attempts, err := db.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, first.ID)
	if err != nil || snapshot.RuleID != "" || snapshot.Payload.Rule.ID != rule.ID || len(attempts) != 1 {
		t.Fatalf("delete erased completed history: %+v attempts=%+v, %v", snapshot, attempts, err)
	}
	if rows, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1); err != nil || len(rows) != 2 {
		t.Fatalf("deleted-rule project history disappeared: %+v, %v", rows, err)
	}
}
