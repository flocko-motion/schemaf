// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

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
	Prefix string   // namespaces migrations in schemaf_migrations, e.g. "schemaf", "myapp"
	Files  embed.FS // embedded migration SQL files
}

var registeredSets []MigrationSet

// RegisterMigrations registers a MigrationSet to be run by RunMigrations.
// Call before RunMigrations; typically in init().
func RegisterMigrations(ms MigrationSet) {
	registeredSets = append(registeredSets, ms)
}

// RunMigrations bootstraps the tracking table if needed, then runs all
// registered migration sets in registration order. Uses the singleton connection.
func RunMigrations(ctx context.Context) error {
	return runMigrations(ctx, conn)
}

// RunMigrationsOn runs migrations on a specific *sql.DB. Used for testing.
func RunMigrationsOn(ctx context.Context, db *sql.DB) error {
	return runMigrations(ctx, db)
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	if err := bootstrap(ctx, db); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	for _, ms := range orderMigrationSets(registeredSets) {
		if err := runSet(ctx, db, ms); err != nil {
			return fmt.Errorf("running migrations for prefix %q: %w", ms.Prefix, err)
		}
	}
	return nil
}

// orderMigrationSets returns a new slice with the "schemaf" prefix always first,
// preserving relative order of all other sets. This guarantees framework migrations
// run before project migrations regardless of registration order.
func orderMigrationSets(sets []MigrationSet) []MigrationSet {
	ordered := make([]MigrationSet, 0, len(sets))
	for _, ms := range sets {
		if ms.Prefix == "schemaf" {
			ordered = append([]MigrationSet{ms}, ordered...)
		} else {
			ordered = append(ordered, ms)
		}
	}
	return ordered
}

// bootstrap ensures schemaf_migrations exists. If it doesn't, it runs
// framework migration 0001 (which creates the table) and records it.
func bootstrap(ctx context.Context, db *sql.DB) error {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'schemaf_migrations'
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking for schemaf_migrations table: %w", err)
	}
	if exists {
		return nil
	}

	// Table doesn't exist: run 0001_init.sql directly, then record it.
	initSQL, err := frameworkMigrations.ReadFile("migrations/0001_init.sql")
	if err != nil {
		return fmt.Errorf("reading framework migration 0001: %w", err)
	}
	if _, err := db.ExecContext(ctx, string(initSQL)); err != nil {
		return fmt.Errorf("running framework migration 0001: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO schemaf_migrations (prefix, version, name) VALUES ('schemaf', 1, 'init')`,
	); err != nil {
		return fmt.Errorf("recording framework migration 0001: %w", err)
	}
	return nil
}

func runSet(ctx context.Context, db *sql.DB, ms MigrationSet) error {
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
		parts := strings.SplitN(d.Name(), "_", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid migration filename %q: expected NNNN_name.sql", d.Name())
		}
		ver, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid version in filename %q: %w", d.Name(), err)
		}
		data, err := ms.Files.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration %q: %w", path, err)
		}
		migrations = append(migrations, migration{
			version: ver,
			name:    strings.TrimSuffix(parts[1], ".sql"),
			sql:     string(data),
		})
		return nil
	})
	if err != nil {
		return err
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	rows, err := db.QueryContext(ctx,
		`SELECT version FROM schemaf_migrations WHERE prefix = $1 ORDER BY version`, ms.Prefix)
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

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if _, err := db.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("running migration %s/%04d_%s: %w", ms.Prefix, m.version, m.name, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schemaf_migrations (prefix, version, name) VALUES ($1, $2, $3)`,
			ms.Prefix, m.version, m.name,
		); err != nil {
			return fmt.Errorf("recording migration %s/%04d_%s: %w", ms.Prefix, m.version, m.name, err)
		}
	}
	return nil
}

//go:embed migrations/*.sql
var frameworkMigrations embed.FS

func init() {
	// Register framework migrations under the "schemaf" prefix.
	// 0001_init.sql is handled specially by bootstrap() on first run.
	RegisterMigrations(MigrationSet{Prefix: "schemaf", Files: frameworkMigrations})
}
