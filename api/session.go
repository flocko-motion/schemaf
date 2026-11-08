// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/flocko-motion/schemaf/constants"
	"github.com/flocko-motion/schemaf/db"
)

// Framework constants for token and session lifetimes. Not configurable.
const (
	TokenLifetime   = 15 * time.Minute
	SessionLifetime = 30 * 24 * time.Hour
)

// SessionStore persists sessions and per-subject revocations.
// The schemaf app injects the Postgres implementation (db.SessionStore).
type SessionStore interface {
	CreateSession(ctx context.Context, s db.Session) error
	GetSession(ctx context.Context, id string) (db.Session, bool, error)
	RevokedBefore(ctx context.Context, subject string) (time.Time, error)
	RevokeSession(ctx context.Context, id string, at time.Time) error
	RevokeSubject(ctx context.Context, subject string, at time.Time) error
}

var sessionStore SessionStore

// InitSessions sets the session store. Called by the schemaf app after the
// database is initialized. Without it, StartSession and identity providers
// are unavailable and only stateless tokens work.
func InitSessions(store SessionStore) {
	sessionStore = store
}

var errNoSessions = errors.New("auth: sessions not initialized (sessions need a database: call app.AddDb in main.go)")

// StartSession starts a session for subject, sets the session cookie on w and
// returns the token for Bearer clients. Use it for logins the app handles
// itself (login codes, guests), from a raw endpoint. The session's provider
// column is empty.
func StartSession(ctx context.Context, w http.ResponseWriter, subject string) (string, error) {
	return startSession(ctx, w, subject, "")
}

func startSession(ctx context.Context, w http.ResponseWriter, subject, provider string) (string, error) {
	if sessionStore == nil {
		return "", errNoSessions
	}
	if subject == "" {
		return "", errors.New("auth: empty subject")
	}
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	t := now()
	sess := db.Session{
		ID:        id,
		Subject:   subject,
		Provider:  provider,
		CreatedAt: t,
		ExpiresAt: t.Add(SessionLifetime),
	}
	if err := sessionStore.CreateSession(ctx, sess); err != nil {
		return "", err
	}
	return issueSessionCookie(w, subject, id, sess.ExpiresAt)
}

// RevokeSession ends one session immediately.
func RevokeSession(ctx context.Context, sessionID string) error {
	if sessionStore == nil {
		return errNoSessions
	}
	return sessionStore.RevokeSession(ctx, sessionID, now())
}

// RevokeSubject immediately ends every session of subject and every stateless
// token issued for it so far.
func RevokeSubject(ctx context.Context, subject string) error {
	if sessionStore == nil {
		return errNoSessions
	}
	return sessionStore.RevokeSubject(ctx, subject, now())
}

// sessionToken issues a token for an existing session, valid for
// TokenLifetime but never beyond the session's expiry.
func sessionToken(subject, sessionID string, sessionExpires time.Time) (string, error) {
	exp := now().Add(TokenLifetime)
	if sessionExpires.Before(exp) {
		exp = sessionExpires
	}
	return issueToken(jwtClaims{Sub: subject, Sid: sessionID}, exp)
}

// issueSessionCookie issues a session token and sets it as the cookie.
func issueSessionCookie(w http.ResponseWriter, subject, sessionID string, sessionExpires time.Time) (string, error) {
	token, err := sessionToken(subject, sessionID, sessionExpires)
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName(),
		Value:    token,
		Path:     "/",
		Expires:  sessionExpires,
		MaxAge:   int(sessionExpires.Sub(now()).Seconds()),
		HttpOnly: true,
		Secure:   secureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
	return token, nil
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

// cookieName is "<project name>_session", so apps on different localhost
// ports don't overwrite each other's cookie.
func cookieName() string {
	if constants.IsProjectNameSet() {
		return SessionCookieName(constants.ProjectName())
	}
	return SessionCookieName("schemaf")
}

func secureCookies() bool { return os.Getenv("SCHEMAF_ENV") == "docker" }

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generating session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// --- Identity providers ---

// IdentityProvider is a login plugin: it verifies a credential in the login
// request and returns the identity it proves. Providers are consulted only at
// login (POST /api/auth/{name}/login); after that only schemaf's own session
// matters.
type IdentityProvider interface {
	Name() string // URL-safe, unique per app
	// Exchange verifies the provider's credential in the login request and
	// returns the identity it proves. ErrForbidden (or wrapped) → 403,
	// ErrBadRequest → 400, any other error → 500.
	Exchange(ctx context.Context, r *http.Request) (Identity, error)
}

// Identity is what an identity provider proves.
type Identity struct {
	Provider string         // set by schemaf
	Subject  string         // the provider's subject (e.g. Auth0 sub)
	Claims   map[string]any // all further claims
}

// OnLoginFunc maps a proven identity to the app's subject. Returning
// ErrForbidden refuses the login (403).
type OnLoginFunc func(ctx context.Context, id Identity) (subject string, err error)

var (
	identityProviders []IdentityProvider
	onLogin           OnLoginFunc
)

// SetIdentityProviders sets the active identity providers. Called by the
// schemaf app at server start, after validating names.
func SetIdentityProviders(ps []IdentityProvider) {
	identityProviders = ps
}

// SetOnLogin sets the identity → subject mapping. nil means the provider's
// subject is used unchanged.
func SetOnLogin(fn OnLoginFunc) {
	onLogin = fn
}

func findProvider(name string) IdentityProvider {
	for _, p := range identityProviders {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// LoginResponse is the body of POST /api/auth/{provider}/login.
type LoginResponse struct {
	Subject string `json:"subject"`
	Token   string `json:"token"`
}

// RefreshResponse is the body of POST /api/auth/refresh.
type RefreshResponse struct {
	Token string `json:"token"`
}

// maxLoginBody caps login request bodies.
const maxLoginBody = 1 << 20

// loginHandler serves POST /api/auth/{provider}/login.
func loginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("provider")
		p := findProvider(name)
		if p == nil {
			writeJSONError(w, http.StatusNotFound, "provider not configured")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
		id, err := p.Exchange(r.Context(), r)
		if err != nil {
			writeJSONError(w, statusFor(err), err.Error())
			return
		}
		id.Provider = name
		if id.Subject == "" {
			writeJSONError(w, http.StatusInternalServerError, "identity provider "+name+" returned an empty subject")
			return
		}
		subject := id.Subject
		if onLogin != nil {
			subject, err = onLogin(r.Context(), id)
			if err != nil {
				writeJSONError(w, statusFor(err), err.Error())
				return
			}
			if subject == "" {
				writeJSONError(w, http.StatusInternalServerError, "OnLogin returned an empty subject")
				return
			}
		}
		token, err := startSession(r.Context(), w, subject, name)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, LoginResponse{Subject: subject, Token: token})
	})
}

// logoutHandler serves POST /api/auth/logout: revokes the current session (if
// any) and clears the cookie. Always 204, except for a cross-origin cookie
// request (403).
func logoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, _, aerr := authenticate(r)
		if aerr != nil && aerr.status == http.StatusForbidden {
			writeJSONError(w, aerr.status, aerr.msg)
			return
		}
		if aerr == nil && info.sessionID != "" {
			if err := sessionStore.RevokeSession(r.Context(), info.sessionID, now()); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		clearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	})
}

// refreshHandler serves POST /api/auth/refresh (AuthRequired): a fresh
// token for the current session.
func refreshHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, _ := r.Context().Value(ctxKeyAuth{}).(authInfo)
		if info.sessionID == "" {
			writeJSONError(w, http.StatusBadRequest, "token is not tied to a session")
			return
		}
		var token string
		var err error
		if info.viaCookie {
			token, err = issueSessionCookie(w, info.subject, info.sessionID, info.sessionExpires)
		} else {
			token, err = sessionToken(info.subject, info.sessionID, info.sessionExpires)
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, RefreshResponse{Token: token})
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
