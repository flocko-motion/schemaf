// reset_test.go — Tests that project table reset works correctly.
// Requires Postgres (started by ./schemaf.sh test).
package db_test

import (
	"context"
	"os"
	"testing"

	frameworkdb "github.com/flocko-motion/schemaf/db"
	"schemaf.local/example/db"
)

// TestResetProjectTablesEmptiesTodos verifies that ResetProjectTables truncates
// project tables (todos) while preserving framework tables (schemaf_migrations).
//
// Requires DATABASE_URL — set automatically by ./schemaf.sh test via the
// ephemeral test postgres container.
func TestResetProjectTablesEmptiesTodos(t *testing.T) {
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

	// Apply all migrations so the schema exists.
	// Framework migrations are registered via init(); register project migrations here.
	frameworkdb.RegisterMigrations(frameworkdb.MigrationSet{
		Prefix: "schemaf-example",
		Files:  db.Provider(),
	})
	if err := frameworkdb.RunMigrationsOn(ctx, sqldb); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	// Insert a row into todos so there is something to reset.
	if _, err := sqldb.ExecContext(ctx, `INSERT INTO todos (text) VALUES ('test-reset')`); err != nil {
		t.Fatalf("insert todo: %v", err)
	}

	frameworkdb.ResetProjectTables(t, ctx, sqldb)

	// todos must be empty.
	var count int
	if err := sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM todos`).Scan(&count); err != nil {
		t.Fatalf("count todos: %v", err)
	}
	if count != 0 {
		t.Errorf("todos not empty after reset: got %d row(s)", count)
	}

	// Framework tracking table must be preserved.
	if err := sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM schemaf_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schemaf_migrations: %v", err)
	}
	if count == 0 {
		t.Error("schemaf_migrations was truncated — framework tables must not be reset")
	}
}
