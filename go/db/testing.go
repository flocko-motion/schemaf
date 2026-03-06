package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// isProjectTable reports whether tableName belongs to the project (not the framework).
// Framework tables are prefixed with "schemaf_" or "_schemaf_" and must be preserved.
func isProjectTable(tableName string) bool {
	return !strings.HasPrefix(tableName, "schemaf_") && !strings.HasPrefix(tableName, "_schemaf_")
}

// ResetProjectTables truncates all non-framework tables in the public schema.
// Framework tables (prefixed with "schemaf_" or "_schemaf_") are preserved.
// Call this at the start of each test that uses a real database to guarantee
// a clean state.
//
// Example:
//
//	func TestMyThing(t *testing.T) {
//	    db.ResetProjectTables(t, ctx, conn)
//	    // ... test body
//	}
func ResetProjectTables(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = 'public'
	`)
	if err != nil {
		t.Fatalf("ResetProjectTables: query tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("ResetProjectTables: scan: %v", err)
		}
		if isProjectTable(name) {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("ResetProjectTables: rows: %v", err)
	}

	for _, table := range tables {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`TRUNCATE TABLE %q CASCADE`, table)); err != nil {
			t.Fatalf("ResetProjectTables: truncate %q: %v", table, err)
		}
	}
}
