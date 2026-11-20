package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flocko-motion/schemaf/db"
)

type nopStore struct{}

func (nopStore) CreateSession(context.Context, db.Session) error { return nil }
func (nopStore) GetSession(context.Context, string) (db.Session, bool, error) {
	return db.Session{}, false, nil
}
func (nopStore) RevokedBefore(context.Context, string) (time.Time, error) { return time.Time{}, nil }
func (nopStore) RevokeSession(context.Context, string, time.Time) error   { return nil }
func (nopStore) RevokeSubject(context.Context, string, time.Time) error   { return nil }

type namedProvider struct{ name string }

func (p namedProvider) Name() string { return p.name }
func (p namedProvider) Exchange(context.Context, *http.Request) (Identity, error) {
	return Identity{}, nil
}

// Reset clears everything a server start sets, so the next start (a
// schemaftest restart or a fresh instance) begins from nothing.
func TestResetClearsServerState(t *testing.T) {
	InitAuth([]byte("key"))
	InitSessions(nopStore{})
	SetIdentityProviders([]IdentityProvider{namedProvider{"p"}})
	SetOnLogin(func(context.Context, Identity) (string, error) { return "", nil })
	SetFrontend(fstest.MapFS{})
	Register(Route{Method: "GET", Path: "/api/x"})
	SetOpenAPISpec("{}")

	Reset()

	if signingKey != nil || sessionStore != nil || identityProviders != nil || onLogin != nil ||
		frontendFS != nil || routes != nil || openAPISpec != "" {
		t.Fatalf("Reset left state: key=%v store=%v providers=%v onLogin=%v frontend=%v routes=%v spec=%q",
			signingKey, sessionStore, identityProviders, onLogin != nil, frontendFS, routes, openAPISpec)
	}
	if _, err := IssueToken("alice", time.Time{}); err == nil {
		t.Fatal("IssueToken works after Reset")
	}
}

// NewHandler(false) never proxies to the dev server, even in development
// mode: without an embedded frontend, a path outside /api/ is a 404.
func TestNewHandlerWithoutDevProxy(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	SetOpenAPISpec(specFor(Routes()))
	t.Setenv("SCHEMAF_DEV", "1")

	h, err := NewHandler(false)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET / = %d %q, want 404", rec.Code, rec.Body.String())
	}

	SetFrontend(fstest.MapFS{"index.html": {Data: []byte("embedded")}})
	h, err = NewHandler(false)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Body.String() != "embedded" {
		t.Fatalf("GET / = %q, want the embedded frontend", rec.Body.String())
	}
}
