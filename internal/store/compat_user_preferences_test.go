//go:build integration

package store

import "testing"

func TestCompatibleUserPreferencesPersist(t *testing.T) {
	ctx, db, owner, _, _ := telemetryStore(t)
	timezone := "UTC"
	clock24 := true
	stackOrder := 2
	updated, err := db.UpdateCompatibleUser(ctx, owner.ID, "Preferred Owner", CompatibleUserOptions{
		Timezone: &timezone, Clock24Hours: &clock24, StacktraceOrder: &stackOrder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Preferred Owner" || updated.Options.Timezone == nil || *updated.Options.Timezone != "UTC" ||
		updated.Options.Clock24Hours == nil || !*updated.Options.Clock24Hours || updated.Options.StacktraceOrder == nil || *updated.Options.StacktraceOrder != 2 {
		t.Fatalf("updated user=%+v", updated)
	}
	if _, err = db.UpdateUserNotifications(ctx, owner.ID, false); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.CompatibleUser(ctx, owner.ID)
	if err != nil || persisted.Name != "Preferred Owner" || persisted.SubscribeByDefault || persisted.Options.Timezone == nil || *persisted.Options.Timezone != "UTC" {
		t.Fatalf("persisted user=%+v err=%v", persisted, err)
	}
}
