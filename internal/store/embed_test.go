//go:build integration

package store

import (
	"errors"
	"testing"
)

func TestEmbeddedUserReportKeyBinding(t *testing.T) {
	f := resolutionStore(t)
	key, projectID := f.project.PublicKey, f.project.ID
	valid, err := f.db.ValidEmbedKey(f.ctx, key, projectID)
	if err != nil || !valid {
		t.Fatalf("valid embed key=%t err=%v", valid, err)
	}
	if valid, err = f.db.ValidEmbedKey(f.ctx, "wrong", projectID); err != nil || valid {
		t.Fatalf("wrong embed key=%t err=%v", valid, err)
	}
	input := UserReportInput{EventID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Embed User", Email: "embed@example.test", Comments: "Embedded feedback"}
	report, err := f.db.SubmitEmbeddedUserReport(f.ctx, key, projectID, input)
	if err != nil || report.ID == "" || report.Comments != input.Comments {
		t.Fatalf("embedded report=%+v err=%v", report, err)
	}
	input.Comments = "Embedded feedback updated"
	again, err := f.db.SubmitEmbeddedUserReport(f.ctx, key, projectID, input)
	if err != nil || again.ID != report.ID || !again.DateCreated.Equal(report.DateCreated) {
		t.Fatalf("embedded retry=%+v err=%v", again, err)
	}
	if _, err = f.db.SubmitEmbeddedUserReport(f.ctx, "wrong", projectID, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong key error=%v", err)
	}
	if _, err = f.db.SubmitEmbeddedUserReport(f.ctx, key, "999999999", input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong project error=%v", err)
	}
	if _, err = f.db.pool.Exec(f.ctx, `UPDATE project_keys SET enabled=false WHERE public_key=$1`, key); err != nil {
		t.Fatal(err)
	}
	if valid, err = f.db.ValidEmbedKey(f.ctx, key, projectID); err != nil || valid {
		t.Fatalf("disabled embed key=%t err=%v", valid, err)
	}
	if _, err = f.db.SubmitEmbeddedUserReport(f.ctx, key, projectID, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled key error=%v", err)
	}
}
