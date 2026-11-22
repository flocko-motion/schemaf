package schemaf

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/flocko-motion/schemaf/api"
)

// The OnBeforeServe hooks run in registration order, before the services
// start and before the server takes requests.
func TestOnBeforeServeRunsInOrderBeforeServices(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(e string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}
	a := &App{ctx: context.Background(), project: "beforeservetest"}
	a.AddApi(func() { api.SetOpenAPISpec(frameworkSpec) })
	started := make(chan struct{})
	a.AddService(func(ctx context.Context) {
		record("service")
		close(started)
		<-ctx.Done()
	})
	ln := listen(t)
	a.OnBeforeServe(func(ctx context.Context) error {
		record("first")
		return nil
	})
	a.OnBeforeServe(func(ctx context.Context) error {
		record("second")
		return nil
	})
	s, err := a.Start(isolatedCtx, ServerOptions{Listener: ln})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer stop(t, s)
	<-started
	wantHealth(t, "http://"+ln.Addr().String())
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"first", "second", "service"}; !slices.Equal(events, want) {
		t.Errorf("events %v, want %v", events, want)
	}
}

// A hook's error aborts the start: the later hooks don't run, no service
// starts, nothing is served, and Start returns the error.
func TestOnBeforeServeErrorAbortsStart(t *testing.T) {
	a := &App{ctx: context.Background(), project: "beforeservetest"}
	a.AddApi(func() { api.SetOpenAPISpec(frameworkSpec) })
	var serviceRan atomic.Bool
	a.AddService(func(ctx context.Context) { serviceRan.Store(true) })
	boom := errors.New("seed failed")
	a.OnBeforeServe(func(ctx context.Context) error { return boom })
	laterRan := false
	a.OnBeforeServe(func(ctx context.Context) error { laterRan = true; return nil })

	ln := listen(t)
	s, err := a.Start(isolatedCtx, ServerOptions{Listener: ln})
	if err == nil {
		stop(t, s)
		t.Fatal("Start succeeded, want the hook's error")
	}
	if !errors.Is(err, boom) || err.Error() != "before-serve hook: seed failed" {
		t.Errorf("error %q, want \"before-serve hook: seed failed\" wrapping the hook's error", err)
	}
	if laterRan {
		t.Error("a hook after the failing one ran")
	}
	if serviceRan.Load() {
		t.Error("a service started after a failing hook")
	}
	if c, err := net.Dial("tcp", ln.Addr().String()); err == nil {
		c.Close()
		t.Error("the listener still accepts connections after a failed start")
	}

	// The failed start left nothing behind: the next server starts.
	b := &App{ctx: context.Background(), project: "beforeservetest"}
	b.AddApi(func() { api.SetOpenAPISpec(frameworkSpec) })
	ln = listen(t)
	s, err = b.Start(isolatedCtx, ServerOptions{Listener: ln})
	if err != nil {
		t.Fatalf("Start after a failed start: %v", err)
	}
	defer stop(t, s)
	wantHealth(t, "http://"+ln.Addr().String())
}
