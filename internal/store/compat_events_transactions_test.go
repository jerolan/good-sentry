//go:build integration

package store

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestCompatibleEventsSnapshotDuringNativeMergeAndSplit(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	base := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	a, root := compatibleCapture(t, ctx, db, project, 1, base, nil)
	b, source := compatibleCapture(t, ctx, db, project, 2, base.Add(time.Second), map[string]any{"fingerprint": []string{"snapshot-source"}})
	writer, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	checkDetail := func() error {
		detail, err := db.CompatibleEvent(ctx, access, EventIssueTarget(source), b.ID, nil)
		if err != nil {
			return err
		}
		if detail.ID != b.ID || !detail.SiblingsKnown || detail.NextID != nil {
			return fmt.Errorf("detail identity/sibling mismatch: %+v", detail)
		}
		switch detail.GroupID {
		case source:
			if detail.PreviousID != nil {
				return fmt.Errorf("split group has sibling from merged topology: %+v", detail)
			}
		case root:
			if detail.PreviousID == nil || *detail.PreviousID != a.ID {
				return fmt.Errorf("merged group has siblings from split topology: %+v", detail)
			}
		default:
			return fmt.Errorf("unexpected detail owner: %+v", detail)
		}
		return nil
	}
	checkList := func() error {
		rows, err := db.CompatibleEvents(ctx, access, EventIssueTarget(source), CompatibleEventFilter{Limit: 100})
		if err != nil {
			return err
		}
		if len(rows) == 1 {
			if rows[0].ID != b.ID || rows[0].GroupID != source {
				return fmt.Errorf("split list uses another topology: %+v", rows)
			}
		} else if len(rows) == 2 {
			if rows[0].ID != b.ID || rows[1].ID != a.ID || rows[0].GroupID != root || rows[1].GroupID != root {
				return fmt.Errorf("merged list uses another topology: %+v", rows)
			}
		} else {
			return fmt.Errorf("list lost or duplicated a partition: %+v", rows)
		}
		return nil
	}
	if err := checkDetail(); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	failures := make(chan error, 4)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for range 60 {
			if _, err := writer.MergeIssues(ctx, owner.ID, org.Slug, []string{root, source}); err != nil {
				failures <- err
				return
			}
			if _, err := writer.UnmergeIssue(ctx, owner.ID, root, []string{source}); err != nil {
				failures <- err
				return
			}
		}
	})
	for range 3 {
		workers.Go(func() {
			<-start
			for range 80 {
				if err := checkDetail(); err != nil {
					failures <- err
					return
				}
				if err := checkList(); err != nil {
					failures <- err
					return
				}
			}
		})
	}
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if err := checkDetail(); err != nil {
		t.Fatal(err)
	}
	if err := checkList(); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibleEventsMigration017PreservesRowsAndReceipts(t *testing.T) {
	f := resolutionStore(t)
	base := time.Date(2024, 4, 5, 6, 7, 8, 0, time.UTC)
	_, root := compatibleCapture(t, f.ctx, f.db, f.project, 1, base, nil)
	_, source := compatibleCapture(t, f.ctx, f.db, f.project, 2, base.Add(time.Second), map[string]any{"fingerprint": []string{"migration-source"}})
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, source}); err != nil {
		t.Fatal(err)
	}
	f.release(t, "app@1")
	f.resolve(t, root, "release", "app@1")
	if _, err := f.db.AddComment(f.ctx, f.owner.ID, root, "Preserve migration history"); err != nil {
		t.Fatal(err)
	}
	restricted, _, _ := apiTokenFixture(t, f.ctx, f.db, f.owner, f.org.Slug, "migration017-restricted", []int64{apiTokenProjectID(t, f.project.ID)})
	revoked, _, _ := apiTokenFixture(t, f.ctx, f.db, f.owner, f.org.Slug, "migration017-revoked", nil)
	if err := f.db.RevokeAPIToken(f.ctx, f.owner.ID, f.org.Slug, revoked.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "UPDATE api_tokens SET expires_at=created_at-interval '1 day' WHERE id=$1", restricted.ID); err != nil {
		t.Fatal(err)
	}
	// Reconstruct only017's prior constraint/receipt in this owned schema. All
	// previously written application rows and migrations001..016 remain real.
	if _, err := f.db.pool.Exec(f.ctx, `ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK (
 cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases']::text[]);
DELETE FROM schema_migrations WHERE version=17`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 16)
	unchangedCatalog := func(db *Store) string {
		t.Helper()
		var catalog map[string]any
		if err := json.Unmarshal([]byte(groupCatalogSnapshot(t, f.ctx, db)), &catalog); err != nil {
			t.Fatal(err)
		}
		constraints := []any{}
		for _, value := range catalog["constraints"].([]any) {
			constraint := value.(map[string]any)
			if constraint["table"] != "api_tokens" || constraint["name"] != "api_tokens_scopes_check" {
				constraints = append(constraints, constraint)
			}
		}
		catalog["constraints"] = constraints
		encoded, err := json.Marshal(catalog)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	beforeCatalog := unchangedCatalog(f.db)
	url := f.db.pool.Config().ConnString()
	f.db.Close()
	upgraded, err := Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	if after := groupDatabaseSnapshot(t, f.ctx, upgraded, 16); !reflect.DeepEqual(before, after) {
		t.Fatal("017 rewrote old application rows or receipts001..016")
	}
	if after := unchangedCatalog(upgraded); after != beforeCatalog {
		t.Fatal("017 changed an index or a constraint other than the token scope allowlist")
	}
	_, hash, prefix := apiTokenCredentials("migration017-event-reader")
	token, err := upgraded.CreateAPIToken(f.ctx, f.owner.ID, f.owner.AuthGeneration, f.org.Slug, APITokenInput{Name: "Event reader", Scopes: []string{"event:read"}, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}, ExpiresInDays: 1}, hash, prefix)
	if err != nil || !reflect.DeepEqual(token.Scopes, []string{"event:read"}) {
		t.Fatalf("017 did not admit implemented event scope: token=%+v err=%v", token, err)
	}
	for _, scope := range []string{"event:write", "event:admin"} {
		tx, err := upgraded.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, rejected := tx.Exec(f.ctx, "UPDATE api_tokens SET scopes=ARRAY[$1]::text[] WHERE id=$2", scope, token.ID)
		_ = tx.Rollback(f.ctx)
		if rejected == nil {
			t.Fatalf("017 accepted unimplemented scope %s", scope)
		}
	}
	beforeRestart := groupDatabaseSnapshot(t, f.ctx, upgraded, 0)
	upgraded.Close()
	again, err := Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	if after := groupDatabaseSnapshot(t, f.ctx, again, 0); !reflect.DeepEqual(beforeRestart, after) {
		t.Fatal("reopen reapplied017 or changed retained rows/receipts")
	}
	if grant, err := again.AuthenticateAPIToken(f.ctx, hash); err != nil || !reflect.DeepEqual(grant.Scopes, []string{"event:read"}) || grant.AllProjects || !reflect.DeepEqual(grant.ProjectIDs, []int64{apiTokenProjectID(t, f.project.ID)}) {
		t.Fatalf("reopen lost new event grant: %+v err=%v", grant, err)
	}
}
