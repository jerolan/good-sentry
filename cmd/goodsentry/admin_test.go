//go:build integration

package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/store"
)

func TestAdminGrantCommandPromotesExistingActiveAccount(t *testing.T) {
	ctx := t.Context()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("goodsentry_admin_cli_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	databaseURL, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := databaseURL.Query()
	query.Set("search_path", schema)
	databaseURL.RawQuery = query.Encode()
	db, err := store.Open(ctx, databaseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	sql, err := pgx.Connect(ctx, databaseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer sql.Close(ctx)
	var userID string
	const email = "cli-grant@example.test"
	if err := sql.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES($1,$2,'CLI target') RETURNING id::text`, email, []byte("cli-hash")).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", databaseURL.String())
	t.Setenv("VALKEY_URL", "")
	if err := runAdmin([]string{"grant", "--email", email}); err != nil {
		t.Fatal(err)
	}
	if err := runAdmin([]string{"grant", "--email", email}); err != nil {
		t.Fatalf("repeat grant was not idempotent: %v", err)
	}
	if err := runAdmin([]string{"grant", "--email", "missing@example.test"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CLI created a missing account: %v", err)
	}
	var adminStatus bool
	if err := sql.QueryRow(ctx, `SELECT is_instance_admin FROM users WHERE id=$1::bigint`, userID).Scan(&adminStatus); err != nil || !adminStatus {
		t.Fatalf("CLI did not grant account: admin=%t err=%v", adminStatus, err)
	}
	if _, err := sql.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1::bigint`, userID); err != nil {
		t.Fatal(err)
	}
	if err := runAdmin([]string{"grant", "--email", email}); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("CLI promoted an inactive account: %v", err)
	}
}
