// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaf

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/cli"
	"github.com/flocko-motion/schemaf/constants"
	"github.com/flocko-motion/schemaf/db"
	slog "github.com/flocko-motion/schemaf/log"
)

// App is a configured schemaf application. Use New to create one.
type App struct {
	ctx         context.Context
	project     string
	hasDB       bool
	subcommands []cli.SubcommandProvider
	services    []func(context.Context)
}

// New creates a new App using the project name registered via constants.SetProjectName.
// The project name determines the database name, host, and migration prefix.
func New(ctx context.Context) *App {
	name := constants.ProjectName()
	return &App{ctx: ctx, project: name}
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

// AddSubcommand registers a subcommand provider. Providers follow the same
// pattern as framework CLI subcommands (see cli/cmd/* for examples).
// Wire up in go/main.go: app.AddSubcommand(importer.SubcommandProvider)
func (a *App) AddSubcommand(provider cli.SubcommandProvider) {
	a.subcommands = append(a.subcommands, provider)
}

// AddService registers a background function that runs as a goroutine when
// the server starts (after DB init and migrations). The context is cancelled
// on server shutdown. Services are not started for CLI subcommands.
// Wire up in go/main.go: app.AddService(myworker.Run)
func (a *App) AddService(fn func(context.Context)) {
	a.services = append(a.services, fn)
}

// SetFrontend registers an embedded frontend filesystem for production serving.
// In dev mode, the server proxies to the frontend dev server on port 7002 instead.
// Wire up in go/main.go: app.SetFrontend(FrontendFS())
func (a *App) SetFrontend(fsys fs.FS) {
	api.SetFrontend(fsys)
}

// Run hands over to Cobra for command routing. The "server" command (default
// when no subcommand is given) starts the HTTP server. Custom subcommands
// registered via AddSubcommand are mounted alongside it.
func (a *App) Run() error {
	// Restore the caller's working directory if schemaf.sh passed it.
	if cwd := os.Getenv("SCHEMAF_CWD"); cwd != "" {
		if err := os.Chdir(cwd); err != nil {
			return fmt.Errorf("chdir to SCHEMAF_CWD: %w", err)
		}
	}

	// Register the DSN for lazy DB initialization. Subcommands that call
	// db.DB() will connect on first use. The server command inits eagerly.
	if a.hasDB {
		db.SetDSN(a.dsn())
	}

	c, err := cli.New()
	if err != nil {
		return fmt.Errorf("init cli: %w", err)
	}

	// Mount the built-in server command as the default action.
	c.AddSubcommands(a.serverProvider)

	// Mount stub commands for schemaf.sh-handled operations so they
	// appear in --help output. Running them directly tells the user
	// to use schemaf.sh instead.
	c.AddSubcommands(shellStubProvider)

	// Mount built-in db commands when a database is registered.
	if a.hasDB {
		c.Root().AddCommand(db.Command())
	}

	// Mount user-registered subcommands.
	c.AddSubcommands(a.subcommands...)

	return c.Execute()
}

// initDB connects to the database and runs migrations.
func (a *App) initDB() error {
	slog.Info("connecting to database", "project", a.project)
	if err := db.Init(a.dsn()); err != nil {
		return fmt.Errorf("db init: %w", err)
	}
	slog.Info("running migrations")
	if err := db.RunMigrations(a.ctx); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	return nil
}

// shellStubProvider registers placeholder commands for operations handled by schemaf.sh.
// They appear in --help but redirect users to schemaf.sh when invoked directly.
func shellStubProvider(_ *cli.Context) []*cobra.Command {
	stub := func(use, short string) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short + " (use ./schemaf.sh)",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("run this via: ./schemaf.sh %s", cmd.Name())
			},
		}
	}

	devCmd := stub("dev", "Start development compose setup")
	devCmd.AddCommand(&cobra.Command{
		Use:   "db",
		Short: "Start only postgres (use ./schemaf.sh)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("run this via: ./schemaf.sh dev db")
		},
	})

	return []*cobra.Command{
		stub("codegen", "Generate all code"),
		stub("test", "Run all tests"),
		stub("run", "Start production compose setup"),
		devCmd,
		stub("upgrade", "Upgrade schemaf to latest version"),
	}
}

// serverProvider returns the built-in "server" command that starts the HTTP server.
func (a *App) serverProvider(_ *cli.Context) []*cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.serve()
		},
	}
	return []*cobra.Command{cmd}
}

// serve eagerly initializes the database and starts the HTTP server.
func (a *App) serve() error {
	if a.hasDB {
		if err := a.initDB(); err != nil {
			return err
		}
		if err := a.initAuth(); err != nil {
			return fmt.Errorf("auth init: %w", err)
		}
	}

	// Launch registered services as background goroutines.
	for _, svc := range a.services {
		go svc(a.ctx)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "7000"
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
