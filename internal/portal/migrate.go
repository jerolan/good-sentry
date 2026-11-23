package portal

import (
	"context"
	_ "embed"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var initialSchema string

// Migrate applies the embedded PostgreSQL schema once per database.
func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// Serialize concurrent invocations before creating the migration table.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(7835462401)"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS goodsentry_migrations (version integer PRIMARY KEY)"); err != nil {
			return err
		}
		for _, migration := range []struct {
			version int
			sql     string
		}{
			{1, initialSchema}, {2, "ALTER TABLE projects ADD COLUMN webhook_url text NOT NULL DEFAULT ''"},
		} {
			var version int
			err := tx.QueryRow(ctx, "SELECT version FROM goodsentry_migrations WHERE version = $1", migration.version).Scan(&version)
			if err == nil {
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if _, err := tx.Exec(ctx, migration.sql); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO goodsentry_migrations (version) VALUES ($1)", migration.version); err != nil {
				return err
			}
		}
		return nil
	})
}

// CheckSchema reports whether the serve command can use the migrated schema.
func CheckSchema(ctx context.Context, db *pgxpool.Pool) error {
	var version int
	return db.QueryRow(ctx, "SELECT version FROM goodsentry_migrations WHERE version = 2").Scan(&version)
}
