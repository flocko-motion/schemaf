package schemaf

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"

	"schemaf.local/base/api"
	"schemaf.local/base/db"
)

// App is a configured schemaf server. Use New to create one.
type App struct {
	ctx     context.Context
	project string
}

// New creates a new App for the given project name (e.g. "schemaf-example").
// The project name determines the database name, host, and migration prefix.
func New(ctx context.Context, project string) *App {
	return &App{ctx: ctx, project: project}
}

// AddMigrations registers an embedded FS of SQL migration files.
// The migration prefix is the full project name (e.g. "schemaf-example").
func (a *App) AddMigrations(migrations embed.FS) {
	db.RegisterMigrations(db.MigrationSet{Prefix: a.project, Files: migrations})
}

// Run connects to the database, runs migrations, and starts the HTTP server.
// It blocks until the server exits.
func (a *App) Run() error {
	dsn := a.dsn()
	log.Printf("connecting to database for project %q", a.project)

	if err := db.Init(dsn); err != nil {
		return fmt.Errorf("db init: %w", err)
	}

	log.Printf("running migrations")
	if err := db.RunMigrations(a.ctx); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "7001"
	}

	log.Printf("starting server on :%s", port)
	return api.Serve(":" + port)
}

// dsn constructs the Postgres DSN deterministically from the project name and environment.
//
// In Docker (SCHEMAF_ENV=docker):
//
//	postgres://schemaf:{DB_PASS}@{project}-postgres:5432/{project}
//
// Native (dev/test):
//
//	postgres://schemaf:dev@localhost:7003/{project}
func (a *App) dsn() string {
	if os.Getenv("SCHEMAF_ENV") == "docker" {
		pass := os.Getenv("DB_PASS")
		return fmt.Sprintf("postgres://schemaf:%s@%s-postgres:5432/%s?sslmode=disable", pass, a.project, a.project)
	}
	// Native: use port 7003 per PORTS.md convention
	return fmt.Sprintf("postgres://schemaf:dev@localhost:7003/%s?sslmode=disable", a.project)
}
