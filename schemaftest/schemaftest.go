// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

// Package schemaftest starts a schemaf app in the test process, against its
// own fresh database, on a free port (EXTEND.md › Integration tests):
//
//	inst := schemaftest.Start(t, app.Wire)
//	inst.URL()           // http://127.0.0.1:<port>
//	inst.DB()            // *sql.DB of this instance's database
//	inst.Token(subject)  // a token signed with the instance's key
//	inst.AuthHeader(tok) // http.Header with "Authorization: Bearer <tok>"
//	inst.Restart()       // graceful stop, then start again on the same database
//
// The first Start in a test binary starts one Postgres container and runs the
// migrations once into a template database; every instance gets its own
// database cloned from it. Instances run one after another, not in parallel.
package schemaftest

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/constants"
	"github.com/flocko-motion/schemaf/internal/isolation"
	"github.com/flocko-motion/schemaf/schemaf"
)

// stopTimeout bounds an instance's graceful stop.
const stopTimeout = 10 * time.Second

// instanceRunning: the server's state is process-wide, so one instance at a
// time per test binary.
var instanceRunning atomic.Bool

const errAnotherInstance = "schemaftest: another instance is still running; instances run one after another, not in parallel"

// Instance is a running app started by Start. It has no exported
// constructor or fields: the only way to get one is Start, which builds its
// database from the container it started.
type Instance struct {
	t      testing.TB
	app    *schemaf.App
	srv    *schemaf.Server
	addr   string // 127.0.0.1:<port>, the same across Restart
	dsn    string
	dbName string
	pool   *sql.DB
}

// Start starts a fresh instance of the app wired by wire: a new database
// with the full schema and no data, then the server, started the way the
// server command starts it but isolated (no OIDC environment providers, no
// backup scheduler, no frontend dev proxy), always in development mode
// (SCHEMAF_DEV=1 via t.Setenv, so the test can't use t.Parallel). t.Cleanup stops the server,
// closes the pool and drops the database. Every failure calls t.Fatal.
//
// Start panics outside a go test binary.
func Start(t testing.TB, wire func(*schemaf.App)) *Instance {
	t.Helper()
	requireTestBinary(testing.Testing())
	if err := checkEnv(os.Getenv); err != nil {
		t.Fatal(err)
	}
	if !constants.IsProjectNameSet() {
		t.Fatal("schemaftest: project name not set: did you forget to run codegen? (go/app/constants.gen.go sets it; the test must import the app package)")
	}
	if !instanceRunning.CompareAndSwap(false, true) {
		t.Fatal(errAnotherInstance)
	}
	// Cleanups run last-in first-out: this one runs after everything below.
	t.Cleanup(func() { instanceRunning.Store(false) })

	// Development mode, always: ./schemaf.sh test sets it and a plain go
	// test doesn't, so the instance mustn't depend on how tests started.
	t.Setenv("SCHEMAF_DEV", "1")

	ctx := context.Background()
	a := schemaf.New(ctx)
	wire(a)

	c, err := ensureContainer()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureTemplate(ctx, c); err != nil {
		t.Fatalf("schemaftest: preparing the template database: %v", err)
	}
	name, err := createDatabase(ctx, c)
	if err != nil {
		t.Fatalf("schemaftest: %v", err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), c, name); err != nil {
			t.Errorf("schemaftest: %v", err)
		}
	})

	inst := &Instance{t: t, app: a, dsn: c.dsn(name), dbName: name}
	if inst.pool, err = sql.Open("postgres", inst.dsn); err != nil {
		t.Fatalf("schemaftest: opening the instance's database: %v", err)
	}
	t.Cleanup(func() { inst.pool.Close() })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("schemaftest: binding a free port: %v", err)
	}
	inst.addr = ln.Addr().String()
	inst.start(ln)
	t.Cleanup(func() {
		if inst.srv != nil {
			if err := inst.stop(); err != nil {
				t.Errorf("schemaftest: stopping the server: %v", err)
			}
		}
	})
	return inst
}

// requireTestBinary panics outside a go test binary, so Start can never run
// in a production binary.
func requireTestBinary(isTesting bool) {
	if !isTesting {
		panic("schemaftest: Start runs only in go test binaries")
	}
}

// checkEnv refuses environments that might be production.
func checkEnv(getenv func(string) string) error {
	if getenv("SCHEMAF_ENV") == "docker" {
		return errors.New("schemaftest: refusing to start with SCHEMAF_ENV=docker (inside a deployed container); integration tests run on the host with go test")
	}
	return nil
}

func (inst *Instance) start(ln net.Listener) {
	inst.t.Helper()
	srv, err := inst.app.Start(isolation.Context(context.Background()), schemaf.ServerOptions{
		Listener: ln,
		DSN:      inst.dsn,
	})
	if err != nil {
		inst.t.Fatalf("schemaftest: starting the server: %v", err)
	}
	inst.srv = srv
}

func (inst *Instance) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	err := inst.srv.Stop(ctx)
	inst.srv = nil
	return err
}

// URL returns the instance's base URL, http://127.0.0.1:<port>. It stays
// the same across Restart.
func (inst *Instance) URL() string { return "http://" + inst.addr }

// DB returns a connection pool on the instance's database, for setting up
// and checking data. It stays valid across Restart.
func (inst *Instance) DB() *sql.DB { return inst.pool }

// Token returns a token for subject, signed with the instance's key in the
// test process: stateless and without expiry (api.IssueToken with a zero
// expiry). api.RevokeSubject still ends it.
func (inst *Instance) Token(subject string) string {
	inst.t.Helper()
	if inst.srv == nil {
		inst.t.Fatal("schemaftest: Token on an instance whose server isn't running")
	}
	tok, err := api.IssueToken(subject, time.Time{})
	if err != nil {
		inst.t.Fatalf("schemaftest: minting a token (does Wire call app.AddDb? the signing key lives in the database): %v", err)
	}
	return tok
}

// AuthHeader returns an http.Header carrying "Authorization: Bearer <tok>",
// e.g. for building the app's generated apiclient.
func (inst *Instance) AuthHeader(tok string) http.Header {
	return http.Header{"Authorization": {"Bearer " + tok}}
}

// Restart stops the server gracefully and starts it again on the same
// database and port. Nothing is kept in memory: data, the signing key and
// sessions survive because they live in the database.
func (inst *Instance) Restart() {
	inst.t.Helper()
	if inst.srv == nil {
		inst.t.Fatal("schemaftest: Restart on an instance whose server isn't running")
	}
	if err := inst.stop(); err != nil {
		inst.t.Fatalf("schemaftest: stopping the server: %v", err)
	}
	ln, err := listenAgain(inst.addr)
	if err != nil {
		inst.t.Fatalf("schemaftest: binding %s again after the stop: %v", inst.addr, err)
	}
	inst.start(ln)
}

// listenAgain binds addr, retrying briefly while the old socket is released.
func listenAgain(addr string) (net.Listener, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil || time.Now().After(deadline) {
			return ln, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
