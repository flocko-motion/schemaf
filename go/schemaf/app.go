package schemaf

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
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
		if err := a.initAuth(); err != nil {
			return fmt.Errorf("auth init: %w", err)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "7001"
	}

	slog.Info("starting server", "addr", ":"+port)
	return api.Serve(":" + port)
}

// initAuth loads (or generates) the JWT signing key from _schemaf_config.
func (a *App) initAuth() error {
	const keyName = "jwt_signing_key"
	val, ok, err := db.ConfigGet(a.ctx, keyName)
	if err != nil {
		return fmt.Errorf("loading jwt signing key: %w", err)
	}
	if !ok {
		// First boot: generate a new random 32-byte key.
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return fmt.Errorf("generating jwt signing key: %w", err)
		}
		val = hex.EncodeToString(raw)
		if err := db.ConfigSet(a.ctx, keyName, val); err != nil {
			return fmt.Errorf("storing jwt signing key: %w", err)
		}
		slog.Info("jwt signing key generated and stored")
	}
	key, err := hex.DecodeString(val)
	if err != nil {
		return fmt.Errorf("decoding jwt signing key: %w", err)
	}
	api.InitAuth(key)
	return nil
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
