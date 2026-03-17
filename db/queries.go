// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package db

import (
	"context"
	"database/sql"
)

const getAppliedMigrations = `-- name: GetAppliedMigrations :many
SELECT version FROM schemaf_migrations WHERE prefix = $1 ORDER BY version
`

func (q *Queries) GetAppliedMigrations(ctx context.Context, prefix string) ([]int32, error) {
	rows, err := q.db.QueryContext(ctx, getAppliedMigrations, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []int32
	for rows.Next() {
		var version int32
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		items = append(items, version)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const insertMigration = `-- name: InsertMigration :exec
INSERT INTO schemaf_migrations (prefix, version, name) VALUES ($1, $2, $3)
`

type InsertMigrationParams struct {
	Prefix  string
	Version int32
	Name    string
}

func (q *Queries) InsertMigration(ctx context.Context, arg InsertMigrationParams) error {
	_, err := q.db.ExecContext(ctx, insertMigration, arg.Prefix, arg.Version, arg.Name)
	return err
}

// DBTX is the interface for database operations.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// New creates a new Queries instance.
func New(db DBTX) *Queries {
	return &Queries{db: db}
}

// Queries holds the database connection.
type Queries struct {
	db DBTX
}
