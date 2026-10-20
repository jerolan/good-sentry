//go:build integration

package store

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

func TestDebugFileStorageDeduplicationAndScope(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	content := []byte("MODULE Linux x86_64 0123456789ABCDEF0 example.sym\n")
	hash := sha1.Sum(content)
	sha := hex.EncodeToString(hash[:])
	file, err := db.SaveDebugFile(ctx, access, project.ID, "example.sym", "sym", "0123456789ABCDEF0", "x86_64", "example.sym", sha, map[string]string{"format": "breakpad"}, map[string]any{"version": 1}, content)
	if err != nil || file.ID == "" || file.DebugID == nil || *file.DebugID != "0123456789ABCDEF0" || file.CPUName != "x86_64" || file.Size != int64(len(content)) || file.SHA1 != sha || file.Headers["format"] != "breakpad" {
		t.Fatalf("debug file save: %+v %v", file, err)
	}
	duplicate, err := db.SaveDebugFile(ctx, access, project.ID, "duplicate.sym", "sym", "0123456789ABCDEF0", "x86_64", "example.sym", sha, nil, nil, content)
	if err != nil || duplicate.ID != file.ID {
		t.Fatalf("debug file dedup: %+v %v", duplicate, err)
	}
	files, err := db.DebugFiles(ctx, access, project.ID, 0, 10)
	if err != nil || len(files) != 1 || files[0].ID != file.ID || files[0].Headers == nil || files[0].Data == nil {
		t.Fatalf("debug file list: %+v %v", files, err)
	}
	second := []byte("second debug file")
	secondHash := sha1.Sum(second)
	if _, err := db.SaveDebugFile(ctx, access, project.ID, "second.sym", "sym", "", "arm64", "second.sym", hex.EncodeToString(secondHash[:]), nil, nil, second); err != nil {
		t.Fatal(err)
	}
	probe, err := db.DebugFiles(ctx, access, project.ID, 0, 1)
	if err != nil || len(probe) != 2 {
		t.Fatalf("debug file probe: %+v %v", probe, err)
	}
	if _, err := db.SaveDebugFile(ctx, access, project.ID, "bad.sym", "sym", "", "", "bad.sym", "not-sha1", nil, nil, content); err == nil {
		t.Fatal("invalid hash accepted")
	}
	foreign := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, Role: "owner", AllProjects: false, ProjectIDs: []int64{0}}
	if _, err := db.DebugFiles(ctx, foreign, project.ID, 0, 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("project scope error: %v", err)
	}
}

func TestDebugFileMigration042PreservesProjectsAndReopens(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if _, err := db.pool.Exec(ctx, `DROP TABLE debug_files; DELETE FROM schema_migrations WHERE version=42`); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM projects p WHERE id=$1::bigint`, project.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		var after, receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM projects p WHERE id=$1::bigint`, project.ID).Scan(&after); err != nil || after != before {
			reopened.Close()
			t.Fatalf("042 changed project: %v", err)
		}
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=42`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/042_debug_files.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("042 checksum absent or changed", err)
		}
		var table string
		if err := reopened.pool.QueryRow(ctx, `SELECT to_regclass('debug_files')::text`).Scan(&table); err != nil || table == "" {
			reopened.Close()
			t.Fatalf("042 table missing: %q %v", table, err)
		}
		reopened.Close()
	}
}
