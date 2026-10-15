// Run from the repository root alongside browser-transaction-span-groups.mjs:
//
//	DATABASE_URL=<fixture database URL> PENDING_REPORT_DIR=<browser output directory> \
//	  go run tests/compat/tools/hold-span-projection.go
//
// Only the browser-generated event reference is changed; original JSON is retained.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	directory := os.Getenv("PENDING_REPORT_DIR")
	if directory == "" {
		return fmt.Errorf("PENDING_REPORT_DIR required")
	}
	waitFile := func(name string) error {
		for {
			if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if err := waitFile("pending-reference.json"); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(directory, "pending-reference.json"))
	if err != nil {
		return err
	}
	var reference struct {
		ProjectID string `json:"projectId"`
		Sample    struct {
			ID     string `json:"id"`
			Source string `json:"source"`
		} `json:"sample"`
	}
	if err := json.Unmarshal(raw, &reference); err != nil {
		return err
	}
	if reference.Sample.Source != "event" || reference.Sample.ID == "" || reference.ProjectID == "" {
		return fmt.Errorf("invalid pending fixture reference")
	}
	connection, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer connection.Close(context.Background())
	var held pgx.Tx
	for attempt := 0; attempt < 10; attempt++ {
		tx, err := connection.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM transaction_span_operations WHERE event_id=$1::bigint", reference.Sample.ID)
		if err == nil {
			_, err = tx.Exec(ctx, "UPDATE events SET span_projection_version=0 WHERE id=$1::bigint AND project_id=$2::bigint AND kind='transaction'", reference.Sample.ID, reference.ProjectID)
		}
		if err != nil {
			tx.Rollback(context.Background())
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		tx, err = connection.Begin(ctx)
		if err != nil {
			return err
		}
		var version int
		err = tx.QueryRow(ctx, "SELECT span_projection_version FROM events WHERE id=$1::bigint AND project_id=$2::bigint FOR UPDATE", reference.Sample.ID, reference.ProjectID).Scan(&version)
		if err != nil {
			tx.Rollback(context.Background())
			return err
		}
		if version == 0 {
			held = tx
			break
		}
		tx.Rollback(context.Background())
	}
	if held == nil {
		return fmt.Errorf("worker reclaimed fixture before lock on all attempts")
	}
	defer held.Rollback(context.Background())
	if err = os.WriteFile(filepath.Join(directory, "indexing-held"), []byte("Fixture event pending and row locked\n"), 0600); err != nil {
		return err
	}
	if err = waitFile("indexing-observed"); err != nil {
		return err
	}
	if err = held.Rollback(ctx); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(directory, "indexing-released"), []byte("Fixture lock released for real application worker\n"), 0600); err != nil {
		return err
	}
	fmt.Println("PASS: pending fixture held through real503 UI and released for in-process indexing")
	return nil
}
