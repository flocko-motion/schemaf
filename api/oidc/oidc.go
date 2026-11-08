// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

// Package oidc implements schemaf's built-in identity providers on top of a
// generic OIDC ID-token verifier (discovery + JWKS via coreos/go-oidc).
//
// Built-ins are activated purely by environment variables (see FromEnv);
// apps don't import this package.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/flocko-motion/schemaf/api"
)

// MaxTokenAge is how old (by iat) an ID token may be when posted to the login
// route. The server can't check the nonce, so this limits replay.
const MaxTokenAge = 5 * time.Minute

// Provider is an identity provider that accepts {"id_token": "<jwt>"} and
// verifies it against an OIDC issuer: signature via the issuer's JWKS, iss,
// aud (must contain the client ID), exp, and iat no older than MaxTokenAge.
type Provider struct {
	name     string
	issuer   string
	clientID string
	public   map[string]string

	// client is used for discovery and JWKS fetches (nil: http.DefaultClient).
	client *http.Client
	// now is the clock (nil: time.Now). For tests.
	now func() time.Time

	mu       sync.Mutex
	verifier *gooidc.IDTokenVerifier
}

// New returns a provider named name for the given issuer URL and client ID.
// public is the config exposed to the frontend via /api/status.
// Discovery happens lazily on first login, so an unreachable issuer does not
// prevent the server from starting.
func New(name, issuer, clientID string, public map[string]string) *Provider {
	return &Provider{name: name, issuer: issuer, clientID: clientID, public: public}
}

// Name implements api.IdentityProvider.
func (p *Provider) Name() string { return p.name }

// PublicConfig implements api.PublicConfigProvider.
func (p *Provider) PublicConfig() map[string]string { return p.public }

func (p *Provider) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// getVerifier runs OIDC discovery once (retrying on later logins if it fails).
func (p *Provider) getVerifier() (*gooidc.IDTokenVerifier, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.verifier != nil {
		return p.verifier, nil
	}
	// The context is kept by go-oidc for background JWKS refreshes, so it must
	// outlive the request.
	ctx := context.Background()
	if p.client != nil {
		ctx = gooidc.ClientContext(ctx, p.client)
	}
	prov, err := gooidc.NewProvider(ctx, p.issuer)
	if err != nil {
		return nil, err
	}
	p.verifier = prov.Verifier(&gooidc.Config{ClientID: p.clientID, Now: p.clock})
	return p.verifier, nil
}

// refusal is a login refusal: 403 with a readable message.
type refusal struct{ msg string }

func (e refusal) Error() string { return e.msg }
func (e refusal) Unwrap() error { return api.ErrForbidden }

// Exchange implements api.IdentityProvider.
func (p *Provider) Exchange(ctx context.Context, r *http.Request) (api.Identity, error) {
	var body struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		return api.Identity{}, fmt.Errorf("%s: invalid request body: %v: %w", p.name, err, api.ErrBadRequest)
	}
	if body.IDToken == "" {
		return api.Identity{}, fmt.Errorf("%s: id_token missing from request body: %w", p.name, api.ErrBadRequest)
	}

	v, err := p.getVerifier()
	if err != nil {
		return api.Identity{}, fmt.Errorf("%s: OIDC discovery for %s failed: %v: %w", p.name, p.issuer, err, api.ErrUnavailable)
	}
	tok, err := v.Verify(ctx, body.IDToken)
	if err != nil {
		return api.Identity{}, refusal{fmt.Sprintf("%s: %v", p.name, err)}
	}
	if tok.IssuedAt.IsZero() {
		return api.Identity{}, refusal{p.name + ": token has no iat"}
	}
	if age := p.clock().Sub(tok.IssuedAt); age > MaxTokenAge {
		return api.Identity{}, refusal{fmt.Sprintf(
			"%s: token issued too long ago (iat %s ago, max %s); post the ID token right after sign-in",
			p.name, age.Truncate(time.Second), MaxTokenAge)}
	}
	var claims map[string]any
	if err := tok.Claims(&claims); err != nil {
		return api.Identity{}, fmt.Errorf("%s: decoding claims: %w", p.name, err)
	}
	return api.Identity{Subject: tok.Subject, Claims: claims}, nil
}

// Builtin describes a built-in identity provider activated by two env vars.
// Further built-ins (keycloak, google, ...) are just more entries.
type Builtin struct {
	Name        string // reserved provider name, e.g. "auth0"
	Title       string // for messages, e.g. "Auth0"
	DomainEnv   string // e.g. "AUTH0_DOMAIN"
	ClientIDEnv string // e.g. "AUTH0_CLIENT_ID"
	Issuer      func(domain string) string
}

// Builtins lists the built-in providers. Their names are reserved.
var Builtins = []Builtin{
	{
		Name:        "auth0",
		Title:       "Auth0",
		DomainEnv:   "AUTH0_DOMAIN",
		ClientIDEnv: "AUTH0_CLIENT_ID",
		Issuer:      func(domain string) string { return "https://" + domain + "/" },
	},
}

// IsReserved reports whether name belongs to a built-in provider.
func IsReserved(name string) bool {
	for _, b := range Builtins {
		if b.Name == name {
			return true
		}
	}
	return false
}

// FromEnv returns the built-in providers activated by the environment.
// For each built-in: neither variable set → off; both set → active; only one
// set → error.
func FromEnv(getenv func(string) string) ([]api.IdentityProvider, error) {
	var out []api.IdentityProvider
	for _, b := range Builtins {
		domain := normalizeDomain(getenv(b.DomainEnv))
		clientID := strings.TrimSpace(getenv(b.ClientIDEnv))
		switch {
		case domain == "" && clientID == "":
			continue
		case domain == "":
			return nil, fmt.Errorf("%s: %s is set but %s is not; set both to enable %s, or neither to disable it",
				b.Name, b.ClientIDEnv, b.DomainEnv, b.Title)
		case clientID == "":
			return nil, fmt.Errorf("%s: %s is set but %s is not; set both to enable %s, or neither to disable it",
				b.Name, b.DomainEnv, b.ClientIDEnv, b.Title)
		}
		out = append(out, New(b.Name, b.Issuer(domain), clientID, map[string]string{
			"domain":    domain,
			"client_id": clientID,
		}))
	}
	return out, nil
}

// normalizeDomain strips a scheme and trailing slashes: "https://x.auth0.com/" → "x.auth0.com".
func normalizeDomain(d string) string {
	d = strings.TrimSpace(d)
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	return strings.TrimRight(d, "/")
}
