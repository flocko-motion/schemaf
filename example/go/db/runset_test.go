// runset_test.go — Tests for frameworkdb.RunSet: applying a single MigrationSet
// to a database without dragging in framework or other registered sets, and the
// per-migration transaction safety that backs it.
// Requires Postgres (started by ./schemaf.sh test).
package db_test

import (
	"context"
	"database/sql"
	"embed"
	"os"
	"testing"

	frameworkdb "github.com/flocko-motion/schemaf/db"
)

//go:embed testdata/runset_widgets/*.sql
var runsetWidgets embed.FS

//go:embed testdata/runset_sentinel/*.sql
var runsetSentinel embed.FS

//go:embed testdata/runset_rollback/*.sql
var runsetRollback embed.FS

// TestRunSetAppliesOnlyGivenSet verifies that RunSet applies exactly the set it
// is handed — creating its tables and tracking its versions under its prefix —
// while ignoring other registered sets (here, a sentinel set). This is the core
// guarantee that lets one MigrationSet target an external/secondary database
// without dragging the rest of the framework schema onto it.
//
// Requires DATABASE_URL — set automatically by ./schemaf.sh test.
func TestRunSetAppliesOnlyGivenSet(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL not set — run tests via ./schemaf.sh test")
	}

	sqldb, err := frameworkdb.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sqldb.Close()

	ctx := context.Background()

	// Clean slate so the test is repeatable on a shared database.
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS runset_widgets`,
		`DROP TABLE IF EXISTS runset_sentinel_marker`,
		`DELETE FROM schemaf_migrations WHERE prefix IN ('runset-widgets', 'runset-sentinel')`,
	} {
		if _, err := sqldb.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("cleanup %q: %v", stmt, err)
		}
	}

	// Register a sentinel set globally. RunSet must NOT apply it.
	frameworkdb.RegisterMigrations(frameworkdb.MigrationSet{
		Prefix: "runset-sentinel",
		Files:  runsetSentinel,
	})

	// Apply ONLY the widgets set.
	if err := frameworkdb.RunSet(ctx, sqldb, frameworkdb.MigrationSet{
		Prefix: "runset-widgets",
		Files:  runsetWidgets,
	}); err != nil {
		t.Fatalf("RunSet: %v", err)
	}

	// The given set's table must exist...
	if !regclassExists(t, ctx, sqldb, "runset_widgets") {
		t.Error("runset_widgets table missing — RunSet did not apply the given set")
	}
	// ...and be tracked under its prefix.
	if n := countMigrations(t, ctx, sqldb, "runset-widgets"); n != 1 {
		t.Errorf("expected 1 tracking row for prefix runset-widgets, got %d", n)
	}

	// The sentinel set must NOT have been touched.
	if regclassExists(t, ctx, sqldb, "runset_sentinel_marker") {
		t.Error("runset_sentinel_marker exists — RunSet leaked into another registered set")
	}
	if n := countMigrations(t, ctx, sqldb, "runset-sentinel"); n != 0 {
		t.Errorf("expected 0 tracking rows for prefix runset-sentinel, got %d", n)
	}

	// Idempotent: a second call must not duplicate tracking rows or fail.
	if err := frameworkdb.RunSet(ctx, sqldb, frameworkdb.MigrationSet{
		Prefix: "runset-widgets",
		Files:  runsetWidgets,
	}); err != nil {
		t.Fatalf("RunSet (second call): %v", err)
	}
	if n := countMigrations(t, ctx, sqldb, "runset-widgets"); n != 1 {
		t.Errorf("after second RunSet, expected 1 tracking row for runset-widgets, got %d", n)
	}
}

// TestRunSetMigrationRollback verifies that a failing migration is rolled back
// atomically: the partial table its first statement created must not persist,
// and no tracking row must be recorded for the failed version. The preceding,
// successful migration in the same set must remain applied.
//
// Requires DATABASE_URL — set automatically by ./schemaf.sh test.
func TestRunSetMigrationRollback(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL not set — run tests via ./schemaf.sh test")
	}

	sqldb, err := frameworkdb.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sqldb.Close()

	ctx := context.Background()

	for _, stmt := range []string{
		`DROP TABLE IF EXISTS runset_rb_good`,
		`DROP TABLE IF EXISTS runset_rb_partial`,
		`DELETE FROM schemaf_migrations WHERE prefix = 'runset-rollback'`,
	} {
		if _, err := sqldb.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("cleanup %q: %v", stmt, err)
		}
	}

	// The set has a good 0001 and a bad 0002 — RunSet must return an error.
	err = frameworkdb.RunSet(ctx, sqldb, frameworkdb.MigrationSet{
		Prefix: "runset-rollback",
		Files:  runsetRollback,
	})
	if err == nil {
		t.Fatal("expected RunSet to fail on the bad migration, got nil")
	}

	// 0001 applied and recorded.
	if !regclassExists(t, ctx, sqldb, "runset_rb_good") {
		t.Error("runset_rb_good missing — the good migration should have committed")
	}
	// 0002 rolled back: its partial table must not exist...
	if regclassExists(t, ctx, sqldb, "runset_rb_partial") {
		t.Error("runset_rb_partial exists — failed migration was not rolled back")
	}
	// ...and only version 1 must be tracked.
	if n := countMigrations(t, ctx, sqldb, "runset-rollback"); n != 1 {
		t.Errorf("expected exactly 1 tracking row (version 1) for runset-rollback, got %d", n)
	}
}

// regclassExists reports whether a table exists in the public schema.
func regclassExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatalf("checking existence of %q: %v", name, err)
	}
	return exists
}

// countMigrations returns the number of schemaf_migrations rows for a prefix.
func countMigrations(t *testing.T, ctx context.Context, db *sql.DB, prefix string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schemaf_migrations WHERE prefix = $1`, prefix).Scan(&n); err != nil {
		t.Fatalf("counting migrations for %q: %v", prefix, err)
	}
	return n
}
