package schemaf

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/internal/isolation"
)

// frameworkSpec declares schemaf's own routes, the spec an app without
// endpoints serves.
const frameworkSpec = `{"openapi":"3.0.3","paths":{
"/api/health":{"get":{}},
"/api/status":{"get":{}},
"/api/config":{"get":{}},
"/api/user/me":{"get":{}},
"/api/auth/{provider}/login":{"post":{}},
"/api/auth/logout":{"post":{}},
"/api/auth/refresh":{"post":{}}}}`

// isolatedCtx starts servers the way schemaftest does.
var isolatedCtx = isolation.Context(context.Background())

// lifecycleApp is an app without a database whose one service records that
// its context was cancelled.
func lifecycleApp(cancelled *atomic.Bool) *App {
	a := &App{ctx: context.Background(), project: "lifecycletest"}
	a.AddApi(func() { api.SetOpenAPISpec(frameworkSpec) })
	a.AddService(func(ctx context.Context) {
		<-ctx.Done()
		cancelled.Store(true)
	})
	return a
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func wantHealth(t *testing.T, base string) {
	t.Helper()
	resp, err := http.Get(base + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/health = %d, want 200", resp.StatusCode)
	}
}

func stop(t *testing.T, s *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// Start serves on the given listener; Stop cancels the services' context,
// waits for them, closes the listener and clears the server's globals.
func TestStartStop(t *testing.T) {
	var cancelled atomic.Bool
	a := lifecycleApp(&cancelled)
	ln := listen(t)
	base := "http://" + ln.Addr().String()

	s, err := a.Start(isolatedCtx, ServerOptions{Listener: ln})
	if err != nil {
		t.Fatal(err)
	}
	wantHealth(t, base)
	stop(t, s)

	if !cancelled.Load() {
		t.Error("the service's context was not cancelled before Stop returned")
	}
	if _, err := http.Get(base + "/api/health"); err == nil {
		t.Error("the server still answers after Stop")
	}
	if api.Routes() != nil {
		t.Error("routes survive Stop")
	}
}

// The same app starts again after a stop: the routes are rebuilt per start.
func TestStartAgainAfterStop(t *testing.T) {
	var cancelled atomic.Bool
	a := lifecycleApp(&cancelled)
	for i := 0; i < 2; i++ {
		ln := listen(t)
		s, err := a.Start(isolatedCtx, ServerOptions{Listener: ln})
		if err != nil {
			t.Fatalf("start %d: %v", i+1, err)
		}
		wantHealth(t, "http://"+ln.Addr().String())
		stop(t, s)
	}
}

// One server per process: a second Start while one runs fails.
func TestStartWhileRunningFails(t *testing.T) {
	var cancelled atomic.Bool
	a := lifecycleApp(&cancelled)
	s, err := a.Start(isolatedCtx, ServerOptions{Listener: listen(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer stop(t, s)
	ln := listen(t)
	defer ln.Close()
	if _, err := a.Start(isolatedCtx, ServerOptions{Listener: ln}); err == nil ||
		!strings.Contains(err.Error(), "already running") {
		t.Fatalf("second Start: %v, want an 'already running' error", err)
	}
}

// A service that ignores its context makes Stop fail at its deadline
// instead of hanging.
func TestStopFailsOnStuckService(t *testing.T) {
	release := make(chan struct{})
	a := &App{ctx: context.Background(), project: "lifecycletest"}
	a.AddApi(func() { api.SetOpenAPISpec(frameworkSpec) })
	a.AddService(func(context.Context) { <-release })
	s, err := a.Start(isolatedCtx, ServerOptions{Listener: listen(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = s.Stop(ctx)
	close(release)
	if err == nil || !strings.Contains(err.Error(), "service") {
		t.Fatalf("Stop = %v, want an error about the service", err)
	}
	// The stuck stop still released the process: a new server can start.
	s, err = a.Start(isolatedCtx, ServerOptions{Listener: listen(t)})
	if err != nil {
		t.Fatalf("Start after a failed Stop: %v", err)
	}
	stop(t, s)
}

// The server command shuts down gracefully on SIGTERM: serve returns nil
// and the services saw their context cancelled.
func TestServeStopsGracefullyOnSIGTERM(t *testing.T) {
	var cancelled atomic.Bool
	a := lifecycleApp(&cancelled)
	ln := listen(t)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	t.Setenv("PORT", strconv.Itoa(port))
	t.Setenv("SCHEMAF_DEV", "")

	done := make(chan error, 1)
	go func() { done <- a.serve() }()

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(base + "/api/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not come up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v after SIGTERM, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not return after SIGTERM")
	}
	if !cancelled.Load() {
		t.Error("the service's context was not cancelled")
	}
}
