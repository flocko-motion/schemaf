package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/flocko-motion/schemaf/api"
)

// fakeIssuer is an in-process OIDC issuer: it serves discovery + JWKS and
// signs its own ID tokens. The signing key can be rotated.
type fakeIssuer struct {
	srv *httptest.Server
	mu  sync.Mutex
	key *rsa.PrivateKey
	kid string
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{}
	f.rotate(t, "k1")
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.srv.URL + "/",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &f.key.PublicKey, KeyID: f.kid, Algorithm: "RS256", Use: "sig",
		}}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIssuer) rotate(t *testing.T, kid string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.key, f.kid = k, kid
	f.mu.Unlock()
}

func (f *fakeIssuer) issuer() string { return f.srv.URL + "/" }

// sign signs claims with the issuer's current key (or with key, if given).
func (f *fakeIssuer) sign(t *testing.T, claims map[string]any, key *rsa.PrivateKey) string {
	t.Helper()
	f.mu.Lock()
	kid := f.kid
	if key == nil {
		key = f.key
	}
	f.mu.Unlock()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (f *fakeIssuer) claims(overrides map[string]any) map[string]any {
	now := time.Now()
	c := map[string]any{
		"iss":   f.issuer(),
		"aud":   "client-123",
		"sub":   "google-oauth2|42",
		"email": "a@example.com",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	for k, v := range overrides {
		c[k] = v
	}
	return c
}

func loginReq(token string) *http.Request {
	body, _ := json.Marshal(map[string]string{"id_token": token})
	return httptest.NewRequest("POST", "/api/auth/test/login", strings.NewReader(string(body)))
}

func TestExchange(t *testing.T) {
	f := newFakeIssuer(t)
	p := New("test", f.issuer(), "client-123", nil)
	p.client = f.srv.Client()

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid token", func(t *testing.T) {
		id, err := p.Exchange(t.Context(), loginReq(f.sign(t, f.claims(nil), nil)))
		if err != nil {
			t.Fatalf("Exchange: %v", err)
		}
		if id.Subject != "google-oauth2|42" {
			t.Errorf("subject = %q", id.Subject)
		}
		if id.Claims["email"] != "a@example.com" {
			t.Errorf("claims missing email: %v", id.Claims)
		}
	})

	refused := []struct {
		name   string
		token  func() string
		reason string
	}{
		{"bad signature", func() string { return f.sign(t, f.claims(nil), otherKey) }, "signature"},
		{"wrong iss", func() string { return f.sign(t, f.claims(map[string]any{"iss": "https://evil.example/"}), nil) }, "issue"},
		{"wrong aud", func() string { return f.sign(t, f.claims(map[string]any{"aud": "other-client"}), nil) }, "audience"},
		{"expired", func() string {
			return f.sign(t, f.claims(map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}), nil)
		}, "expired"},
		{"iat older than 5 min", func() string {
			return f.sign(t, f.claims(map[string]any{"iat": time.Now().Add(-6 * time.Minute).Unix()}), nil)
		}, "too long ago"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.Exchange(t.Context(), loginReq(tc.token()))
			if !errors.Is(err, api.ErrForbidden) {
				t.Fatalf("err = %v, want ErrForbidden", err)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("err %q does not mention %q", err, tc.reason)
			}
		})
	}

	t.Run("missing id_token is a bad request", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
		if _, err := p.Exchange(t.Context(), r); !errors.Is(err, api.ErrBadRequest) {
			t.Fatalf("err = %v, want ErrBadRequest", err)
		}
	})

	t.Run("JWKS key rotation", func(t *testing.T) {
		f.rotate(t, "k2")
		id, err := p.Exchange(t.Context(), loginReq(f.sign(t, f.claims(nil), nil)))
		if err != nil {
			t.Fatalf("Exchange after rotation: %v", err)
		}
		if id.Subject != "google-oauth2|42" {
			t.Errorf("subject = %q", id.Subject)
		}
	})
}

func TestFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	t.Run("none set: off", func(t *testing.T) {
		ps, err := FromEnv(env(nil))
		if err != nil || len(ps) != 0 {
			t.Fatalf("got %v, %v; want none", ps, err)
		}
	})

	for _, half := range []map[string]string{
		{"AUTH0_DOMAIN": "x.auth0.com"},
		{"AUTH0_CLIENT_ID": "abc"},
	} {
		t.Run("half set: error", func(t *testing.T) {
			_, err := FromEnv(env(half))
			if err == nil || !strings.Contains(err.Error(), "set both to enable Auth0, or neither to disable it") {
				t.Fatalf("err = %v", err)
			}
		})
	}

	t.Run("both set: active", func(t *testing.T) {
		ps, err := FromEnv(env(map[string]string{"AUTH0_DOMAIN": "https://x.eu.auth0.com/", "AUTH0_CLIENT_ID": "abc"}))
		if err != nil || len(ps) != 1 {
			t.Fatalf("got %v, %v", ps, err)
		}
		p := ps[0].(*Provider)
		if p.Name() != "auth0" || p.issuer != "https://x.eu.auth0.com/" || p.clientID != "abc" {
			t.Errorf("provider = %+v", p)
		}
		cfg := p.PublicConfig()
		if len(cfg) != 2 || cfg["domain"] != "x.eu.auth0.com" || cfg["client_id"] != "abc" {
			t.Errorf("public config = %v", cfg)
		}
	})
}
