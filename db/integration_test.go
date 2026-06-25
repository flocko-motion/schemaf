// integration_test.go — Real-Postgres tests for the migration runner and the
// test-reset helper. These test framework behavior directly (no consumer
// project needed). They are skipped unless DATABASE_URL is set; run them with
// an ephemeral database via e2e/db-test.sh.
package db

import (
	"context"
	"database/sql"
	"embed"
	"os"
	"testing"
)

//go:embed testdata/runset_widgets/*.sql
var runsetWidgets embed.FS

//go:embed testdata/runset_sentinel/*.sql
var runsetSentinel embed.FS

//go:embed testdata/runset_rollback/*.sql
var runsetRollback embed.FS

// openIntegrationDB opens the test database, skipping the test if DATABASE_URL
// is unset so plain `go test ./...` (no database) stays green.
func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test (run e2e/db-test.sh)")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, ctx context.Context, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
}

// resetSchema wipes the public schema so each integration test starts from a
// clean, isolated database (the harness gives every run a dedicated postgres).
func resetSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	mustExec(t, ctx, db, `DROP SCHEMA public CASCADE`, `CREATE SCHEMA public`)
}

func regclassExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatalf("checking existence of %q: %v", name, err)
	}
	return exists
}

func countMigrations(t *testing.T, ctx context.Context, db *sql.DB, prefix string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schemaf_migrations WHERE prefix = $1`, prefix).Scan(&n); err != nil {
		t.Fatalf("counting migrations for %q: %v", prefix, err)
	}
	return n
}

// TestRunSetAppliesOnlyGivenSet verifies RunSet applies exactly the set handed
// to it — creating its tables and tracking its versions — while ignoring other
// registered sets (a sentinel). It also confirms idempotency on a second call.
func TestRunSetAppliesOnlyGivenSet(t *testing.T) {
	db := openIntegrationDB(t)
	ctx := context.Background()

	resetSchema(t, ctx, db)

	// Register a sentinel set globally — RunSet must NOT apply it.
	RegisterMigrations(MigrationSet{Prefix: "runset-sentinel", Files: runsetSentinel})

	if err := RunSet(ctx, db, MigrationSet{Prefix: "runset-widgets", Files: runsetWidgets}); err != nil {
		t.Fatalf("RunSet: %v", err)
	}

	if !regclassExists(t, ctx, db, "runset_widgets") {
		t.Error("runset_widgets missing — RunSet did not apply the given set")
	}
	if n := countMigrations(t, ctx, db, "runset-widgets"); n != 1 {
		t.Errorf("expected 1 tracking row for runset-widgets, got %d", n)
	}
	if regclassExists(t, ctx, db, "runset_sentinel_marker") {
		t.Error("runset_sentinel_marker exists — RunSet leaked into another registered set")
	}
	if n := countMigrations(t, ctx, db, "runset-sentinel"); n != 0 {
		t.Errorf("expected 0 tracking rows for runset-sentinel, got %d", n)
	}

	// Idempotent: second call must not duplicate or fail.
	if err := RunSet(ctx, db, MigrationSet{Prefix: "runset-widgets", Files: runsetWidgets}); err != nil {
		t.Fatalf("RunSet (second call): %v", err)
	}
	if n := countMigrations(t, ctx, db, "runset-widgets"); n != 1 {
		t.Errorf("after second RunSet, expected 1 tracking row, got %d", n)
	}
}

// TestRunSetMigrationRollback verifies a failing migration is rolled back
// atomically: the partial table its first statement created must not persist,
// and no tracking row is recorded for the failed version, while the preceding
// good migration stays applied.
func TestRunSetMigrationRollback(t *testing.T) {
	db := openIntegrationDB(t)
	ctx := context.Background()

	resetSchema(t, ctx, db)

	err := RunSet(ctx, db, MigrationSet{Prefix: "runset-rollback", Files: runsetRollback})
	if err == nil {
		t.Fatal("expected RunSet to fail on the bad migration, got nil")
	}

	if !regclassExists(t, ctx, db, "runset_rb_good") {
		t.Error("runset_rb_good missing — the good migration should have committed")
	}
	if regclassExists(t, ctx, db, "runset_rb_partial") {
		t.Error("runset_rb_partial exists — failed migration was not rolled back")
	}
	if n := countMigrations(t, ctx, db, "runset-rollback"); n != 1 {
		t.Errorf("expected exactly 1 tracking row (version 1) for runset-rollback, got %d", n)
	}
}

// TestResetProjectTables verifies ResetProjectTables truncates project tables
// while preserving framework tables (schemaf_migrations).
func TestResetProjectTables(t *testing.T) {
	db := openIntegrationDB(t)
	ctx := context.Background()

	resetSchema(t, ctx, db)

	if err := RunSet(ctx, db, MigrationSet{Prefix: "runset-widgets", Files: runsetWidgets}); err != nil {
		t.Fatalf("RunSet: %v", err)
	}
	mustExec(t, ctx, db, `INSERT INTO runset_widgets (name) VALUES ('x')`)

	ResetProjectTables(t, ctx, db)

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runset_widgets`).Scan(&count); err != nil {
		t.Fatalf("count runset_widgets: %v", err)
	}
	if count != 0 {
		t.Errorf("runset_widgets not empty after reset: %d row(s)", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schemaf_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schemaf_migrations: %v", err)
	}
	if count == 0 {
		t.Error("schemaf_migrations was truncated — framework tables must be preserved")
	}
}
