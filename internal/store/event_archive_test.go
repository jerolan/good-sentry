//go:build integration

package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// The local error archive is a durable copy. Readers must prefer the hot row,
// then fall back to the archived copy without duplicates; expiry removes only
// the copy and leaves issue topology/comments untouched.
func TestEventArchiveCopyUnionRestartAndExpiry(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	ev := groupEvent(t, 701, "event-archive", "Archived error receipt", at)
	issueID, fresh, err := db.Ingest(ctx, project.ID, ev)
	if err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE event_id=$1`, ev.ID); err != nil {
		t.Fatal(err)
	}
	archived, err := db.ArchiveEvents(ctx, time.Now().UTC().Add(-24*time.Hour), 250)
	if err != nil || archived != 1 {
		t.Fatalf("archived=%d err=%v", archived, err)
	}
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	assertEventArchiveReaders(t, ctx, db, owner.ID, access, project.ID, issueID, ev.ID, 1)

	// Remove the hot copy to prove the archive-backed readers are real. This
	// event has no children, so the delete does not disturb unrelated rows.
	if _, err := db.pool.Exec(ctx, `DELETE FROM events WHERE project_id=$1::bigint AND event_id=$2`, project.ID, ev.ID); err != nil {
		t.Fatal(err)
	}
	assertEventArchiveReaders(t, ctx, db, owner.ID, access, project.ID, issueID, ev.ID, 1)

	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertEventArchiveReaders(t, ctx, reopened, owner.ID, access, project.ID, issueID, ev.ID, 1)

	if _, err := reopened.pool.Exec(ctx, `UPDATE event_archive SET created_at=now()-interval '40 days' WHERE id=$1`, evRowID(t, ctx, reopened, ev.ID)); err != nil {
		t.Fatal(err)
	}
	removed, err := reopened.CleanupEventArchive(ctx, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	assertEventArchiveReaders(t, ctx, reopened, owner.ID, access, project.ID, issueID, ev.ID, 0)
}

func assertEventArchiveReaders(t *testing.T, ctx context.Context, db *Store, userID string, access ManagementAccess, projectID, issueID, eventID string, want int) {
	t.Helper()
	detail, err := db.IssueDetail(ctx, userID, issueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != want {
		t.Fatalf("IssueDetail events=%d want=%d", len(detail.Events), want)
	}
	if want == 1 {
		var payload map[string]any
		if err := json.Unmarshal(detail.Event, &payload); err != nil || payload["event_id"] != eventID {
			t.Fatalf("IssueDetail payload=%s err=%v", detail.Event, err)
		}
	}
	projectEvents, err := db.CompatibleEvents(ctx, access, EventProjectTarget(projectID), CompatibleEventFilter{Limit: 100})
	if err != nil || len(projectEvents) != want {
		t.Fatalf("project events=%d want=%d err=%v", len(projectEvents), want, err)
	}
	issueEvents, err := db.CompatibleEvents(ctx, access, EventIssueTarget(issueID), CompatibleEventFilter{Limit: 100})
	if err != nil || len(issueEvents) != want {
		t.Fatalf("issue events=%d want=%d err=%v", len(issueEvents), want, err)
	}
	if want == 1 && (projectEvents[0].ID != eventID || issueEvents[0].ID != eventID) {
		t.Fatalf("archive reader returned wrong ID: %+v %+v", projectEvents[0], issueEvents[0])
	}
}

func evRowID(t *testing.T, ctx context.Context, db *Store, eventID string) int64 {
	t.Helper()
	var id int64
	if err := db.pool.QueryRow(ctx, `SELECT id FROM event_archive WHERE event_id=$1`, eventID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
