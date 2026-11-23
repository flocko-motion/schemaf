// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaf

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/api/oidc"
	"github.com/flocko-motion/schemaf/cli"
	"github.com/flocko-motion/schemaf/constants"
	"github.com/flocko-motion/schemaf/db"
	"github.com/flocko-motion/schemaf/files"
	slog "github.com/flocko-motion/schemaf/log"
)

// App is a configured schemaf application. Use New to create one.
type App struct {
	ctx         context.Context
	project     string
	hasDB       bool
	subcommands []cli.SubcommandProvider
	services    []func(context.Context)
	beforeServe []func(context.Context) error

	identityProviders  []api.IdentityProvider
	onLogin            api.OnLoginFunc
	canSeeErrorDetails api.CanSeeErrorDetailsFunc
	updatesAuth        api.UpdatesAuthFunc

	// onRequest is the OnRequest hook; onRequestErr a wrong registration
	// (twice, or nil), reported by Start.
	onRequest    api.OnRequestFunc
	onRequestErr error

	// onResponseHeaders is the OnResponseHeaders hook; onResponseHeadersErr
	// a wrong registration (twice, or nil), reported by Start.
	onResponseHeaders    api.ResponseHeadersFunc
	onResponseHeadersErr error

	// apiProviders and frontend are applied by Start, which rebuilds the
	// routes on every start.
	apiProviders []func()
	frontend     fs.FS

	// projectRoot is the checkout (where schemaf.toml is), found before
	// SCHEMAF_CWD moves the working directory; "" if there is none.
	projectRoot string
	// instanceFlag is the --instance value ("" when not given).
	instanceFlag string
}

// New creates a new App using the project name registered via constants.SetProjectName.
// The project name determines the database name, host, and migration prefix.
func New(ctx context.Context) *App {
	name := constants.ProjectName()
	return &App{ctx: ctx, project: name}
}

// AddApi registers the API endpoints: the server calls the generated
// provider function on every start.
// Wire up in Wire (go/app/app.go): app.AddApi(api.Provider)
func (a *App) AddApi(provider func()) {
	a.apiProviders = append(a.apiProviders, provider)
}

// AddDb registers the project's database migrations.
// Wire up in Wire (go/app/app.go): app.AddDb(db.Provider)
func (a *App) AddDb(provider func() embed.FS) {
	a.hasDB = true
	db.RegisterMigrations(db.MigrationSet{Prefix: a.project, Files: provider()})
}

// AddSubcommand registers a subcommand provider. Providers follow the same
// pattern as framework CLI subcommands (see cli/cmd/* for examples).
// Wire up in Wire (go/app/app.go): app.AddSubcommand(importer.SubcommandProvider)
func (a *App) AddSubcommand(provider cli.SubcommandProvider) {
	a.subcommands = append(a.subcommands, provider)
}

// AddService registers a background function that runs as a goroutine when
// the server starts (after DB init, migrations and the OnBeforeServe hooks).
// The context is cancelled when the server stops, and the server waits for
// the function to return.
// Services are not started for CLI subcommands.
// Wire up in Wire (go/app/app.go): app.AddService(myworker.Run)
func (a *App) AddService(fn func(context.Context)) {
	a.services = append(a.services, fn)
}

// OnBeforeServe registers a hook that runs on every server start, after the
// database is opened and migrated and auth is initialised, and before the
// services start and the server takes requests. Hooks run in registration
// order; the first error aborts the start (Run and Start return it as
// "before-serve hook: …") and nothing is served. It runs for the server
// command and schemaftest.Start, never for codegen or subcommands. See
// EXTEND.md, "Before-serve hook". Wire up in Wire (go/app/app.go):
//
//	app.OnBeforeServe(func(ctx context.Context) error {
//	    return core.Bootstrap(ctx)
//	})
func (a *App) OnBeforeServe(fn func(ctx context.Context) error) {
	a.beforeServe = append(a.beforeServe, fn)
}

// AddIdentityProvider registers a custom identity provider. Its login route is
// POST /api/auth/{Name()}/login. A reserved name (a built-in such as "auth0")
// or a duplicate name makes the server fail at startup.
// Wire up in Wire (go/app/app.go): app.AddIdentityProvider(auth.PasskeyProvider{})
func (a *App) AddIdentityProvider(p api.IdentityProvider) {
	a.identityProviders = append(a.identityProviders, p)
}

// OnLogin sets the optional hook that maps a proven identity to the app's
// subject at login. Without it, the provider's subject is used unchanged.
// Returning api.ErrForbidden refuses the login (403). Claims such as email
// are only available here.
func (a *App) OnLogin(fn func(ctx context.Context, id api.Identity) (subject string, err error)) {
	a.onLogin = fn
}

// OnRequest registers the per-request hook: it runs on every API call after
// authentication and before the handler. Its admission (api.Admission) is
// the authenticated caller, handed only to this hook and valid only while it
// runs. The returned context is the one the handler gets; a returned error
// refuses the request like an endpoint error. Calling it twice, or with nil,
// is a startup error. See EXTEND.md, "Per-request hook". Wire up in Wire
// (go/app/app.go):
//
//	app.OnRequest(func(ctx context.Context, a *api.Admission) (context.Context, error) {
//	    subject, ok := a.Subject()
//	    caller, err := users.Resolve(ctx, subject, ok)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return users.WithCaller(ctx, caller), nil
//	})
func (a *App) OnRequest(fn func(ctx context.Context, adm *api.Admission) (context.Context, error)) {
	switch {
	case a.onRequestErr != nil: // keep the first wrong registration
	case fn == nil:
		a.onRequestErr = errors.New("app.OnRequest called with nil: pass the hook, or don't call OnRequest")
	case a.onRequest != nil:
		a.onRequestErr = errors.New("app.OnRequest called twice: register one hook and call your own steps from it")
	default:
		a.onRequest = fn
	}
}

// OnResponseHeaders registers the response-headers hook: it runs on every
// response the server writes (endpoints, schemaf's routes, /openapi.json,
// the frontend, the route gate's 404 and 405, errors), before routing and
// authentication, so it sees no subject. It adds headers to h; a handler runs
// after it and may override them. The X-Schemaf- prefix is reserved: a hook
// that sets, changes or deletes an X-Schemaf-* header fails the request with
// a 500. Calling it twice, or with nil, is a startup error. See EXTEND.md,
// "Response headers". Wire up in Wire (go/app/app.go):
//
//	app.OnResponseHeaders(func(r *http.Request, h http.Header) {
//	    h.Set("X-Content-Type-Options", "nosniff")
//	})
func (a *App) OnResponseHeaders(fn func(r *http.Request, h http.Header)) {
	switch {
	case a.onResponseHeadersErr != nil: // keep the first wrong registration
	case fn == nil:
		a.onResponseHeadersErr = errors.New("app.OnResponseHeaders called with nil: pass the hook, or don't call OnResponseHeaders")
	case a.onResponseHeaders != nil:
		a.onResponseHeadersErr = errors.New("app.OnResponseHeaders called twice: register one hook and set all headers from it")
	default:
		a.onResponseHeaders = fn
	}
}

// CanSeeErrorDetails sets who may look up the details of a server error
// (GET /api/errors/{id}) in production. Without it, nobody may: details are
// off by default. In development mode every caller may, and fn is not asked.
// Wire up in Wire (go/app/app.go):
//
//	app.CanSeeErrorDetails(func(ctx context.Context, subject string) bool {
//	    return users.IsAdmin(ctx, subject)
//	})
func (a *App) CanSeeErrorDetails(fn func(ctx context.Context, subject string) bool) {
	a.canSeeErrorDetails = fn
}

// SetUpdatesAuth sets who may use the update endpoints, GET /api/updates,
// POST /api/updates/request and POST /api/updates/check. Without it, nobody
// may: all three answer 403. fn runs
// after authentication and the OnRequest hook, so it sees the app's caller; a
// returned error refuses the request (api.Error(api.ErrForbidden, …) is a
// 403). See EXTEND.md, "Updates". Wire up in Wire (go/app/app.go):
//
//	app.SetUpdatesAuth(func(ctx context.Context) error {
//	    if !users.FromContext(ctx).IsAdmin {
//	        return api.Error(api.ErrForbidden, "forbidden", "only admins manage updates")
//	    }
//	    return nil
//	})
func (a *App) SetUpdatesAuth(fn func(ctx context.Context) error) {
	a.updatesAuth = fn
}

// SetFrontend registers an embedded frontend filesystem for production serving.
// In development mode (api.DevMode), the server proxies to the frontend dev server instead.
// Wire up in Wire (go/app/app.go): app.SetFrontend(FrontendFS())
func (a *App) SetFrontend(fsys fs.FS) {
	a.frontend = fsys
}

// Run hands over to Cobra for command routing. The "server" command (default
// when no subcommand is given) starts the HTTP server. Custom subcommands
// registered via AddSubcommand are mounted alongside it.
func (a *App) Run() error {
	// Refuse an ambiguous SCHEMAF_DEV before any command runs, so api.DevMode
	// only ever sees "1" (development) or unset/empty (production).
	if v := os.Getenv("SCHEMAF_DEV"); v != "" && v != "1" {
		return fmt.Errorf("SCHEMAF_DEV=%q is not a valid value: use \"1\" for development mode, or leave it unset or empty for production", v)
	}

	// The checkout holds .schemaf/instance. schemaf.sh starts the binary in
	// the project root, so find it before moving to the caller's directory.
	if root, err := files.ProjectRoot(); err == nil {
		a.projectRoot = root
	}

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

	a.addInstanceFlag(c.Root())

	// Mount built-in commands.
	c.AddSubcommands(a.serverProvider)
	c.AddSubcommands(a.devProvider)
	c.AddSubcommands(a.instancesProvider)

	// Mount stub commands for schemaf.sh-handled operations so they
	// appear in --help output. Running them directly tells the user
	// to use schemaf.sh instead.
	c.AddSubcommands(shellStubProvider)

	// Mount built-in db + auth commands when a database is registered.
	if a.hasDB {
		c.Root().AddCommand(db.Command())
		c.AddSubcommands(a.authProvider)
	}

	// Mount user-registered subcommands.
	c.AddSubcommands(a.subcommands...)

	// Every command runs on its dev instance (INSTALL.md › Parallel dev
	// instances): it moves the ports, the DSN, the data directory and the
	// session cookie. Resolved before cobra runs any hook, so no command,
	// app subcommands with their own PersistentPreRun included, can miss it.
	if err := a.resolveInstanceFor(c.Root(), os.Args[1:]); err != nil {
		return err
	}

	return c.Execute()
}

// addInstanceFlag adds the persistent --instance flag to the root command.
func (a *App) addInstanceFlag(root *cobra.Command) {
	root.PersistentFlags().StringVar(&a.instanceFlag, "instance", "",
		"dev instance: a number ≥ 1, or \"default\" (dev only; sticks to the checkout via dev)")
}

// resolveInstanceFor finds the command args name and parses its flags the way
// Execute will, then resolves its instance (resolveInstance). A command line
// cobra can't parse resolves nothing: Execute then reports the same error and
// runs no command.
func (a *App) resolveInstanceFor(root *cobra.Command, args []string) error {
	cmd, rest, err := root.Find(args)
	if err != nil {
		return nil
	}
	if err := cmd.ParseFlags(rest); err != nil {
		return nil
	}
	return a.resolveInstance(cmd)
}

// resolveInstance selects the command's dev instance from --instance and the
// checkout's .schemaf/instance, then points the DSN at it. Outside
// development mode (production, the server in its container) instances don't
// exist: the file is ignored and the flag refused.
func (a *App) resolveInstance(cmd *cobra.Command) error {
	if !api.DevMode() {
		if a.instanceFlag != "" {
			return fmt.Errorf("--instance is dev-only: production (build, run, the server in its container) always runs the one production stack")
		}
		return nil
	}
	n := 0
	if a.projectRoot != "" {
		var err error
		if n, err = files.ResolveInstance(a.projectRoot, a.instanceFlag, isDevCommand(cmd)); err != nil {
			return err
		}
	} else if a.instanceFlag != "" {
		return fmt.Errorf("--instance needs a checkout: no schemaf.toml found here or in any parent directory")
	}
	if err := constants.SetInstance(n); err != nil {
		return err
	}
	if n != 0 {
		fmt.Fprintf(os.Stderr, "instance %d\n", n)
	}
	if a.hasDB {
		db.SetDSN(a.dsn())
	}
	return nil
}

// isDevCommand reports whether cmd is the built-in dev command, the only one
// that may switch a checkout's instance.
func isDevCommand(cmd *cobra.Command) bool {
	return cmd.Name() == "dev" && cmd.Parent() != nil && !cmd.Parent().HasParent()
}

// initDB connects to the database at dsn and runs migrations.
func (a *App) initDB(ctx context.Context, dsn string) error {
	slog.Info("connecting to database", "project", a.project)
	if err := db.Init(dsn); err != nil {
		return fmt.Errorf("db init: %w", err)
	}
	slog.Info("running migrations")
	if err := db.RunMigrations(ctx); err != nil {
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

	return []*cobra.Command{
		stub("codegen", "Generate all code"),
		stub("test", "Run all tests"),
		stub("build", "Build the production image"),
		stub("run", "Start production compose setup"),
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

// shutdownTimeout bounds the graceful stop on SIGINT/SIGTERM: inside the 10
// seconds a container runtime gives a container to stop.
const shutdownTimeout = 8 * time.Second

// serve runs the server command: Start on the instance's port, then a
// graceful Stop on SIGINT or SIGTERM.
func (a *App) serve() error {
	sigCtx, stopSignals := signal.NotifyContext(a.ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	// A parallel dev instance always listens on its own base port; PORT (e.g.
	// from the shared dev env file) would collide with the default instance.
	port := os.Getenv("PORT")
	if port == "" || constants.Instance() != 0 {
		port = strconv.Itoa(constants.Port())
	}
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("listen on :%s: %w", port, err)
	}
	var dsn string
	if a.hasDB {
		dsn = a.dsn()
	}
	srv, err := a.Start(a.ctx, ServerOptions{Listener: ln, DSN: dsn})
	if err != nil {
		return err
	}

	var serveErr error
	select {
	case <-sigCtx.Done():
		slog.Info("shutting down")
	case serveErr = <-srv.Err():
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return errors.Join(serveErr, srv.Stop(ctx))
}

// providerNameRe matches URL-safe provider names.
var providerNameRe = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// identityProvidersFor returns the active identity providers: the built-ins
// activated by the environment, then the custom ones registered via
// AddIdentityProvider. It fails on half-configured built-ins, invalid,
// reserved or duplicate names, and on any active provider without a database.
func (a *App) identityProvidersFor(getenv func(string) string) ([]api.IdentityProvider, error) {
	providers, err := oidc.FromEnv(getenv)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range providers {
		seen[p.Name()] = true
	}
	for _, p := range a.identityProviders {
		name := p.Name()
		switch {
		case !providerNameRe.MatchString(name):
			return nil, fmt.Errorf("identity provider name %q is not URL-safe (allowed: letters, digits, . _ ~ -)", name)
		case oidc.IsReserved(name):
			return nil, fmt.Errorf("identity provider name %q is reserved for the built-in provider", name)
		case seen[name]:
			return nil, fmt.Errorf("identity provider %q registered twice", name)
		}
		seen[name] = true
		providers = append(providers, p)
	}
	if len(providers) > 0 && !a.hasDB {
		names := make([]string, len(providers))
		for i, p := range providers {
			names[i] = p.Name()
		}
		return nil, fmt.Errorf("identity providers (%s) need a database for sessions: call app.AddDb(db.Provider) in Wire (go/app/app.go)",
			strings.Join(names, ", "))
	}
	return providers, nil
}

// initAuth loads (or generates) the JWT signing key from _schemaf_config.
func (a *App) initAuth(ctx context.Context) error {
	const keyName = "jwt_signing_key"
	val, ok, err := db.ConfigGet(ctx, keyName)
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
		if err := db.ConfigSet(ctx, keyName, val); err != nil {
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
//	postgres://schemaf:{DB_PASS}@{DB_HOST|postgres}:5432/{project}
//
// Native (dev/test):
//
//	postgres://schemaf:{DB_PASS|dev}@127.0.0.1:{port+3}/{project}
func (a *App) dsn() string {
	if os.Getenv("SCHEMAF_ENV") == "docker" {
		pass := os.Getenv("DB_PASS")
		host := os.Getenv("DB_HOST")
		if host == "" {
			host = "postgres"
		}
		return fmt.Sprintf("postgres://schemaf:%s@%s:5432/%s?sslmode=disable", pass, host, a.project)
	}
	// Native: Explicit 127.0.0.1 — Docker binds to IPv4 only, and "localhost" may resolve to IPv6.
	pass := os.Getenv("DB_PASS")
	if pass == "" {
		pass = "dev"
	}
	return fmt.Sprintf("postgres://schemaf:%s@127.0.0.1:%d/%s?sslmode=disable", pass, constants.PostgresPort(), a.project)
}
