// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/db"
	"github.com/flocko-motion/schemaf/internal/isolation"
	slog "github.com/flocko-motion/schemaf/log"
)

// ServerOptions configures App.Start.
type ServerOptions struct {
	// Listener is where the server accepts connections. Required. Stop
	// closes it.
	Listener net.Listener
	// DSN is the database the server runs on. Required when the app has a
	// database (AddDb); ignored otherwise.
	DSN string
}

// running guards the process-wide server state (signing key, sessions,
// database pool, routes): one server at a time per process.
var running atomic.Bool

// Server is a server started by App.Start. Stop it with Stop.
type Server struct {
	http     *http.Server
	cancel   context.CancelFunc
	services sync.WaitGroup
	serveErr chan error
	stopOnce sync.Once
	stopErr  error
}

// Start starts the app's server: it connects to opts.DSN and runs the
// migrations, loads the signing key (generating it on first boot), starts the
// session store and its cleanup, activates the identity providers and the
// OnLogin hook, starts the AddService services and serves on opts.Listener.
// The services and the session cleanup run on a context derived from ctx,
// which Stop cancels. schemaftest marks ctx as isolated (internal/isolation):
// no OIDC environment providers, no backup scheduler, no frontend dev proxy.
//
// The server's state is process-wide, so only one server runs at a time: a
// second Start before Stop fails. Used by the server command and by
// schemaftest.
func (a *App) Start(ctx context.Context, opts ServerOptions) (*Server, error) {
	if opts.Listener == nil {
		return nil, errors.New("schemaf: Start needs a listener")
	}
	if !running.CompareAndSwap(false, true) {
		return nil, errors.New("schemaf: a server is already running in this process; stop it first")
	}
	s := &Server{serveErr: make(chan error, 1)}
	ctx, s.cancel = context.WithCancel(ctx)
	if err := a.start(ctx, s, opts); err != nil {
		s.cancel()
		s.services.Wait()
		opts.Listener.Close()
		clearServerState()
		running.Store(false)
		return nil, err
	}
	return s, nil
}

// start does Start's work; on error, Start undoes it.
func (a *App) start(ctx context.Context, s *Server, opts ServerOptions) error {
	api.Reset() // every start begins from nothing
	isolated := isolation.Isolated(ctx)
	providers, err := a.identityProvidersFor(envForProviders(isolated))
	if err != nil {
		return err
	}

	if a.hasDB {
		if opts.DSN == "" {
			return errors.New("schemaf: Start needs a DSN for an app with a database")
		}
		if err := a.initDB(ctx, opts.DSN); err != nil {
			return err
		}
		if err := a.initAuth(ctx); err != nil {
			return fmt.Errorf("auth init: %w", err)
		}
		sessions := db.NewSessionStore(db.DB())
		api.InitSessions(sessions)
		s.goService(ctx, sessions.RunSessionCleanup)
	}
	api.SetIdentityProviders(providers)
	api.SetOnLogin(a.onLogin)
	if api.DevMode() {
		slog.Info("server mode", "mode", "development")
	} else {
		slog.Info("server mode", "mode", "production")
	}
	if len(providers) > 0 {
		names := make([]string, len(providers))
		for i, p := range providers {
			names[i] = p.Name()
		}
		slog.Info("identity providers active", "providers", names)
	}

	// Auto-start the backup scheduler if SFTP is configured.
	if !isolated {
		if cfg, ok := db.ReadSFTPConfigFromEnv(); ok {
			dsn := opts.DSN
			s.goService(ctx, func(ctx context.Context) { db.RunBackupScheduler(ctx, dsn, cfg) })
		}
	}

	for _, svc := range a.services {
		s.goService(ctx, svc)
	}

	// The routes are rebuilt on every start: the API providers register them
	// again on a registry Stop (or a failed Start) left empty.
	for _, provide := range a.apiProviders {
		provide()
	}
	if a.frontend != nil {
		api.SetFrontend(a.frontend)
	}
	handler, err := api.NewHandler(api.DevMode() && !isolated)
	if err != nil {
		return err
	}
	s.http = &http.Server{Handler: handler}
	slog.Info("starting server", "addr", opts.Listener.Addr().String())
	go func() {
		if err := s.http.Serve(opts.Listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.serveErr <- err
		}
	}()
	return nil
}

// getenv reads the process environment; the built-in identity providers of
// a server that isn't isolated are read from it.
var getenv = os.Getenv

// envForProviders returns the environment the built-in identity providers
// are read from: none for an isolated server.
func envForProviders(isolated bool) func(string) string {
	if isolated {
		return func(string) string { return "" }
	}
	return getenv
}

// goService runs fn on ctx as a service Stop waits for.
func (s *Server) goService(ctx context.Context, fn func(context.Context)) {
	s.services.Add(1)
	go func() {
		defer s.services.Done()
		fn(ctx)
	}()
}

// Err receives the error if the server stops serving on its own (not
// through Stop).
func (s *Server) Err() <-chan error { return s.serveErr }

// Stop stops the server gracefully: it cancels the services' context, shuts
// the HTTP server down (no new connections, in-flight requests finish), waits
// for the services to return, closes the database pool and clears the
// process-wide server state (signing key, sessions, identity providers,
// OnLogin, routes, frontend, database). ctx bounds the shutdown and the wait:
// a service still running at its deadline makes Stop fail, but the teardown
// is complete either way and a new server can start.
func (s *Server) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() { s.stopErr = s.stop(ctx) })
	return s.stopErr
}

func (s *Server) stop(ctx context.Context) error {
	var errs []error
	s.cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("shutting down the HTTP server: %w", err))
		s.http.Close()
	}
	done := make(chan struct{})
	go func() {
		s.services.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	select {
	case <-done: // the services returned, even if ctx ran out at the same time
	default:
		errs = append(errs, errors.New("a service did not return after its context was cancelled (AddService functions must return once ctx is done)"))
	}
	if err := clearServerState(); err != nil {
		errs = append(errs, err)
	}
	running.Store(false)
	return errors.Join(errs...)
}

// clearServerState clears every process-wide piece of server state.
func clearServerState() error {
	api.Reset()
	if err := db.Close(); err != nil {
		return fmt.Errorf("closing the database pool: %w", err)
	}
	return nil
}
