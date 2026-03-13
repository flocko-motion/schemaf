// migrate_idempotency_test.go — Verifies migrations can be re-applied safely.
// Requires Postgres (started by ./schemaf.sh test).
package db_test

import (
	"context"
	"os"
	"testing"

	frameworkdb "github.com/flocko-motion/schemaf/db"
	"schemaf.local/example/db"
)

// TestMigrationIdempotency verifies that running migrations twice does not
// insert duplicate rows into schemaf_migrations — i.e., already-applied
// migrations are skipped on subsequent runs.
//
// Requires DATABASE_URL — set automatically by ./schemaf.sh test.
func TestMigrationIdempotency(t *testing.T) {
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

	frameworkdb.RegisterMigrations(frameworkdb.MigrationSet{
		Prefix: "schemaf-example",
		Files:  db.Provider(),
	})

	// First run: apply all migrations.
	if err := frameworkdb.RunMigrationsOn(ctx, sqldb); err != nil {
		t.Fatalf("first migration run: %v", err)
	}

	var countAfterFirst int
	if err := sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM schemaf_migrations`).Scan(&countAfterFirst); err != nil {
		t.Fatalf("count after first run: %v", err)
	}
	if countAfterFirst == 0 {
		t.Fatal("expected schemaf_migrations to have rows after first run")
	}

	// Second run: all migrations already applied — must not insert duplicates.
	if err := frameworkdb.RunMigrationsOn(ctx, sqldb); err != nil {
		t.Fatalf("second migration run: %v", err)
	}

	var countAfterSecond int
	if err := sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM schemaf_migrations`).Scan(&countAfterSecond); err != nil {
		t.Fatalf("count after second run: %v", err)
	}
	if countAfterSecond != countAfterFirst {
		t.Fatalf("migration idempotency broken: %d rows after first run, %d after second run",
			countAfterFirst, countAfterSecond)
	}
}
