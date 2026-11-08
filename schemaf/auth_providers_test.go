package schemaf

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/api"
)

type stubProvider string

func (p stubProvider) Name() string { return string(p) }
func (p stubProvider) Exchange(context.Context, *http.Request) (api.Identity, error) {
	return api.Identity{}, nil
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func names(ps []api.IdentityProvider) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name())
	}
	return strings.Join(out, ",")
}

func TestIdentityProvidersFor(t *testing.T) {
	full := map[string]string{"AUTH0_DOMAIN": "x.eu.auth0.com", "AUTH0_CLIENT_ID": "abc"}

	t.Run("no env, no providers", func(t *testing.T) {
		a := &App{}
		ps, err := a.identityProvidersFor(envOf(nil))
		if err != nil || len(ps) != 0 {
			t.Fatalf("got %q, %v", names(ps), err)
		}
	})

	t.Run("auth0 half configured: startup error", func(t *testing.T) {
		a := &App{hasDB: true}
		_, err := a.identityProvidersFor(envOf(map[string]string{"AUTH0_DOMAIN": "x.eu.auth0.com"}))
		want := "auth0: AUTH0_DOMAIN is set but AUTH0_CLIENT_ID is not; set both to enable Auth0, or neither to disable it"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("auth0 fully configured: active, before custom providers", func(t *testing.T) {
		a := &App{hasDB: true}
		a.AddIdentityProvider(stubProvider("passkey"))
		ps, err := a.identityProvidersFor(envOf(full))
		if err != nil || names(ps) != "auth0,passkey" {
			t.Fatalf("got %q, %v", names(ps), err)
		}
	})

	t.Run("provider without database: startup error", func(t *testing.T) {
		a := &App{}
		if _, err := a.identityProvidersFor(envOf(full)); err == nil || !strings.Contains(err.Error(), "need a database") {
			t.Fatalf("auth0 without db: err = %v", err)
		}
		a.AddIdentityProvider(stubProvider("passkey"))
		if _, err := a.identityProvidersFor(envOf(nil)); err == nil || !strings.Contains(err.Error(), "need a database") {
			t.Fatalf("custom without db: err = %v", err)
		}
	})

	for name, ps := range map[string][]string{
		"reserved name":  {"auth0"},
		"duplicate name": {"passkey", "passkey"},
		"unsafe name":    {"pass/key"},
	} {
		t.Run(name+": startup error", func(t *testing.T) {
			a := &App{hasDB: true}
			for _, n := range ps {
				a.AddIdentityProvider(stubProvider(n))
			}
			if _, err := a.identityProvidersFor(envOf(nil)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
