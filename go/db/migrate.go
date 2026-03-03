package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// MigrationSet holds a set of migration files namespaced by prefix.
type MigrationSet struct {
	Prefix string   // e.g. "ab", "todo" — namespaces migrations in ab_migrations
	Files  embed.FS // embedded migration SQL files
}

var registeredSets []MigrationSet

// RegisterMigrations registers a MigrationSet to be run by RunMigrations.
// Call before RunMigrations; typically in init().
func RegisterMigrations(ms MigrationSet) {
	registeredSets = append(registeredSets, ms)
}

// RunMigrations creates the ab_migrations table if needed, then runs all
// registered migration sets in registration order. Uses the singleton connection.
func RunMigrations(ctx context.Context) error {
	db := conn
	return runMigrations(ctx, db)
}

// RunMigrationsOn runs migrations on a specific *sql.DB. Used for testing.
func RunMigrationsOn(ctx context.Context, db *sql.DB) error {
	return runMigrations(ctx, db)
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	// Always ensure the tracking table exists first
	const createTable = `
CREATE TABLE IF NOT EXISTS ab_migrations (
    id         SERIAL PRIMARY KEY,
    prefix     TEXT NOT NULL,
    version    INT NOT NULL,
    name       TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(prefix, version)
)`
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("creating ab_migrations table: %w", err)
	}

	for _, ms := range registeredSets {
		if err := runSet(ctx, db, ms); err != nil {
			return fmt.Errorf("running migrations for prefix %q: %w", ms.Prefix, err)
		}
	}
	return nil
}

func runSet(ctx context.Context, db *sql.DB, ms MigrationSet) error {
	// Collect all .sql files from the embedded FS
	type migration struct {
		version int
		name    string
		sql     string
	}

	var migrations []migration

	err := fs.WalkDir(ms.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}
		base := d.Name()
		// Parse version from NNNN_name.sql
		parts := strings.SplitN(base, "_", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid migration filename %q: expected NNNN_name.sql", base)
		}
		ver, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid version in filename %q: %w", base, err)
		}
		name := strings.TrimSuffix(parts[1], ".sql")

		data, err := ms.Files.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration %q: %w", path, err)
		}

		migrations = append(migrations, migration{version: ver, name: name, sql: string(data)})
		return nil
	})
	if err != nil {
		return err
	}

	// Sort by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	// Get applied versions for this prefix
	rows, err := db.QueryContext(ctx, `SELECT version FROM ab_migrations WHERE prefix = $1 ORDER BY version`, ms.Prefix)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Run unapplied migrations in order
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if _, err := db.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("running migration %s/%04d_%s: %w", ms.Prefix, m.version, m.name, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO ab_migrations (prefix, version, name) VALUES ($1, $2, $3)`,
			ms.Prefix, m.version, m.name,
		); err != nil {
			return fmt.Errorf("recording migration %s/%04d_%s: %w", ms.Prefix, m.version, m.name, err)
		}
	}

	return nil
}

//go:embed migrations/*.sql
var abMigrations embed.FS

func init() {
	RegisterMigrations(MigrationSet{Prefix: "ab", Files: abMigrations})
}
