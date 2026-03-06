package schemaf

import (
	"context"
	"embed"
	"fmt"
	"os"

	"schemaf.local/base/api"
	"schemaf.local/base/db"
	slog "schemaf.local/base/log"
)

// App is a configured schemaf server. Use New to create one.
type App struct {
	ctx     context.Context
	project string
	hasDB   bool
}

// New creates a new App for the given project name (e.g. "schemaf-example").
// The project name determines the database name, host, and migration prefix.
func New(ctx context.Context, project string) *App {
	return &App{ctx: ctx, project: project}
}

// AddApi registers all API endpoints by calling the generated provider function.
// Wire up in go/main.go: app.AddApi(api.Provider)
func (a *App) AddApi(provider func()) {
	provider()
}

// AddDb registers the project's database migrations.
// Wire up in go/main.go: app.AddDb(db.Provider)
func (a *App) AddDb(provider func() embed.FS) {
	a.hasDB = true
	db.RegisterMigrations(db.MigrationSet{Prefix: a.project, Files: provider()})
}

// Run starts the HTTP server, connecting to the database first if AddDb was called.
// Blocks until the server exits.
func (a *App) Run() error {
	if a.hasDB {
		slog.Info("connecting to database", "project", a.project)
		if err := db.Init(a.dsn()); err != nil {
			return fmt.Errorf("db init: %w", err)
		}
		slog.Info("running migrations")
		if err := db.RunMigrations(a.ctx); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "7001"
	}

	slog.Info("starting server", "addr", ":"+port)
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
