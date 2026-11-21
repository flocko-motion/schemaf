package schemaf

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/flocko-motion/schemaf/api"
)

// An OnRequest hook registered on the app runs for the started server.
func TestOnRequestRunsOnStartedServer(t *testing.T) {
	var cancelled atomic.Bool
	a := lifecycleApp(&cancelled)
	var calls atomic.Int32
	a.OnRequest(func(ctx context.Context, _ *api.Admission) (context.Context, error) {
		calls.Add(1)
		return ctx, nil
	})
	ln := listen(t)
	s, err := a.Start(isolatedCtx, ServerOptions{Listener: ln})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer stop(t, s)
	wantHealth(t, "http://"+ln.Addr().String())
	if calls.Load() != 1 {
		t.Errorf("hook ran %d times, want 1", calls.Load())
	}
}

// Calling OnRequest twice, or with nil, is a startup error naming the fix.
func TestOnRequestRegistrationErrors(t *testing.T) {
	hook := func(ctx context.Context, _ *api.Admission) (context.Context, error) { return ctx, nil }
	for name, tc := range map[string]struct {
		register func(a *App)
		want     string
	}{
		"twice": {func(a *App) { a.OnRequest(hook); a.OnRequest(hook) }, "app.OnRequest called twice"},
		"nil":   {func(a *App) { a.OnRequest(nil) }, "app.OnRequest called with nil"},
	} {
		t.Run(name, func(t *testing.T) {
			var cancelled atomic.Bool
			a := lifecycleApp(&cancelled)
			tc.register(a)
			s, err := a.Start(isolatedCtx, ServerOptions{Listener: listen(t)})
			if err == nil {
				stop(t, s)
				t.Fatal("Start succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A hook refusing everything does not carry over to the next server of an
// app without one.
func TestOnRequestNotCarriedToNextServer(t *testing.T) {
	var cancelled atomic.Bool
	a := lifecycleApp(&cancelled)
	a.OnRequest(func(ctx context.Context, _ *api.Admission) (context.Context, error) {
		return nil, errors.New("refuse everything")
	})
	s, err := a.Start(isolatedCtx, ServerOptions{Listener: listen(t)})
	if err != nil {
		t.Fatal(err)
	}
	stop(t, s)

	b := lifecycleApp(&cancelled)
	ln := listen(t)
	s, err = b.Start(isolatedCtx, ServerOptions{Listener: ln})
	if err != nil {
		t.Fatal(err)
	}
	defer stop(t, s)
	resp, err := http.Get("http://" + ln.Addr().String() + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("health = %d after a hooked server stopped, want 200", resp.StatusCode)
	}
}
