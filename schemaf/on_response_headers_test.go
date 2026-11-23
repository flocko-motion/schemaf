// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaf

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/flocko-motion/schemaf/api"
)

// An OnResponseHeaders hook registered on the app adds its headers to the
// started server's responses, next to X-Schemaf-Version.
func TestOnResponseHeadersRunsOnStartedServer(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", "")
	var cancelled atomic.Bool
	a := lifecycleApp(&cancelled)
	a.OnResponseHeaders(func(_ *http.Request, h http.Header) {
		h.Set("X-Content-Type-Options", "nosniff")
	})
	ln := listen(t)
	s, err := a.Start(isolatedCtx, ServerOptions{Listener: ln})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer stop(t, s)
	resp, err := http.Get("http://" + ln.Addr().String() + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("X-Schemaf-Version"); got != "dev" {
		t.Errorf("X-Schemaf-Version = %q, want dev", got)
	}
}

// Calling OnResponseHeaders twice, or with nil, is a startup error naming the fix.
func TestOnResponseHeadersRegistrationErrors(t *testing.T) {
	hook := func(*http.Request, http.Header) {}
	for name, tc := range map[string]struct {
		register func(a *App)
		want     string
	}{
		"twice": {func(a *App) { a.OnResponseHeaders(hook); a.OnResponseHeaders(hook) }, "app.OnResponseHeaders called twice"},
		"nil":   {func(a *App) { a.OnResponseHeaders(nil) }, "app.OnResponseHeaders called with nil"},
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

// A hook does not carry over to the next server of an app without one.
func TestOnResponseHeadersNotCarriedToNextServer(t *testing.T) {
	var cancelled atomic.Bool
	a := lifecycleApp(&cancelled)
	a.OnResponseHeaders(func(_ *http.Request, h http.Header) { h.Set("X-Hooked", "1") })
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
	if got := resp.Header.Get("X-Hooked"); got != "" {
		t.Errorf("X-Hooked = %q after a hooked server stopped, want none", got)
	}
}

// The dev command's placeholder image reports the version dev.
func TestNativeDevImageIsVersionDev(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", nativeDevImage())
	if got := api.RunningVersion(); got != "dev" {
		t.Errorf("RunningVersion() = %q with the dev placeholder %q, want dev", got, nativeDevImage())
	}
}
