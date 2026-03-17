// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"

	slog "github.com/flocko-motion/schemaf/log"
)

const (
	maxOpenConns    = 25
	maxIdleConns    = 5
	connMaxLifetime = 5 * time.Minute
)

// conn is the package-level singleton database connection pool.
var conn *sql.DB

// dsnValue stores the DSN for lazy initialization.
var dsnValue string

// SetDSN stores the DSN for lazy initialization. The actual connection
// is deferred until the first call to DB().
func SetDSN(dsn string) {
	dsnValue = dsn
}

// Init opens a new Postgres connection pool, pings the server, and stores it
// as the package-level singleton. Use this for eager initialization (e.g. server startup).
func Init(dsn string) error {
	db, err := open(dsn)
	if err != nil {
		return err
	}
	conn = db
	return nil
}

// DB returns the raw *sql.DB singleton. If the connection hasn't been
// opened yet but a DSN was registered via SetDSN, it connects lazily
// (without running migrations).
func DB() *sql.DB {
	if conn == nil && dsnValue != "" {
		if err := Init(dsnValue); err != nil {
			slog.Error("database connection failed", "error", err)
			os.Exit(1)
		}
	}
	return conn
}

// open opens and pings a new Postgres connection pool.
func open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging db: %w", err)
	}

	return db, nil
}

// Open opens a new Postgres connection pool and returns it. Deprecated: prefer Init.
func Open(dsn string) (*sql.DB, error) {
	return open(dsn)
}

// QueryContext executes a query that returns rows using the singleton connection.
func QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return conn.QueryContext(ctx, query, args...)
}

// QueryRowContext executes a query that returns a single row using the singleton connection.
func QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return conn.QueryRowContext(ctx, query, args...)
}

// ExecContext executes a query that doesn't return rows using the singleton connection.
func ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return conn.ExecContext(ctx, query, args...)
}

// BeginTx starts a transaction using the singleton connection.
func BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return conn.BeginTx(ctx, opts)
}

// HealthCheck pings the singleton database and returns an error if unhealthy.
func HealthCheck() error {
	if conn == nil {
		return fmt.Errorf("db not initialized")
	}
	if err := conn.Ping(); err != nil {
		return fmt.Errorf("db ping failed: %w", err)
	}
	return nil
}
