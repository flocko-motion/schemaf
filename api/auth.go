// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ctxKeyAuth is the context key for the authenticated caller (authInfo).
type ctxKeyAuth struct{}

// authInfo is what requireAuth stores in the request context.
type authInfo struct {
	subject        string
	sessionID      string    // "" for stateless tokens
	sessionExpires time.Time // zero for stateless tokens
	viaCookie      bool
}

var signingKey []byte

// now is the clock used for tokens and sessions; replaced in tests.
var now = time.Now

// InitAuth sets the HMAC-SHA256 signing key used for JWT verification.
// Called by the schemaf app after migrations run and the key is loaded from _schemaf_config.
func InitAuth(key []byte) {
	signingKey = key
}

// Subject returns the authenticated subject (user ID) from the context.
// Returns ("", false) if the request was not authenticated.
func Subject(ctx context.Context) (string, bool) {
	info, ok := ctx.Value(ctxKeyAuth{}).(authInfo)
	if !ok {
		return "", false
	}
	return info.subject, true
}

// SessionID returns the session ID of the authenticated request.
// Returns ("", false) if the request was not authenticated or used a
// stateless token (see IssueToken).
func SessionID(ctx context.Context) (string, bool) {
	info, ok := ctx.Value(ctxKeyAuth{}).(authInfo)
	if !ok || info.sessionID == "" {
		return "", false
	}
	return info.sessionID, true
}

// jwtClaims is the minimal set of claims schemaf uses.
type jwtClaims struct {
	Sub string `json:"sub"`
	Sid string `json:"sid,omitempty"` // session ID; empty for stateless tokens
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp,omitempty"`
}

// IssueToken creates a signed stateless JWT for the given subject. exp=0 means
// no expiry. The token is not tied to a session: it is never renewed and only
// RevokeSubject ends it early. Use it for service-to-service tokens; use
// StartSession for users.
func IssueToken(sub string, exp time.Time) (string, error) {
	return issueToken(jwtClaims{Sub: sub}, exp)
}

func issueToken(claims jwtClaims, exp time.Time) (string, error) {
	if len(signingKey) == 0 {
		return "", errors.New("auth: signing key not initialized")
	}
	header := base64url(mustJSON(map[string]string{"alg": "HS256", "typ": "JWT"}))
	claims.Iat = now().Unix()
	if !exp.IsZero() {
		claims.Exp = exp.Unix()
	}
	payload := base64url(mustJSON(claims))
	sig := sign(header + "." + payload)
	return header + "." + payload + "." + sig, nil
}

var errTokenExpired = errors.New("auth: token expired")

// verifyToken parses and verifies a JWT, returning the claims on success.
// An expired but otherwise valid token returns its claims together with
// errTokenExpired, so the caller can decide (cookie renewal).
func verifyToken(token string) (jwtClaims, error) {
	if len(signingKey) == 0 {
		return jwtClaims{}, errors.New("auth: signing key not initialized")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, errors.New("auth: malformed token")
	}
	expected := sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(parts[2]), []byte(expected)) {
		return jwtClaims{}, errors.New("auth: invalid signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, fmt.Errorf("auth: decode payload: %w", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return jwtClaims{}, fmt.Errorf("auth: unmarshal claims: %w", err)
	}
	if claims.Exp != 0 && now().Unix() > claims.Exp {
		return claims, errTokenExpired
	}
	return claims, nil
}

// authError is an authentication failure with the HTTP status to answer.
type authError struct {
	status int
	msg    string
}

func (e *authError) Error() string { return e.msg }

func unauthorized(msg string) *authError { return &authError{http.StatusUnauthorized, msg} }

// authenticate verifies the request's token (Bearer wins over the cookie),
// the Origin check for cookie requests, and the session or subject-level
// revocation. It never writes to w. renew reports whether a cookie-borne
// session token should be replaced by a fresh one.
func authenticate(r *http.Request) (info authInfo, renew bool, aerr *authError) {
	var token string
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimPrefix(h, "Bearer ")
	} else if c, err := r.Cookie(cookieName()); err == nil && c.Value != "" {
		token = c.Value
		info.viaCookie = true
	} else {
		return info, false, unauthorized("missing credentials: no Bearer token or session cookie")
	}

	// CSRF: cookies are sent by the browser automatically, so a state-changing
	// cookie request must come from our own origin. Bearer is exempt.
	if info.viaCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
		return info, false, &authError{http.StatusForbidden, "cross-origin request rejected: Origin header missing or not matching the host"}
	}

	claims, err := verifyToken(token)
	expired := errors.Is(err, errTokenExpired)
	if err != nil && !expired {
		return info, false, unauthorized(err.Error())
	}
	// An expired token is only accepted from the cookie, for a live session
	// (it is renewed below). Everything else that expired is rejected.
	if expired && !(info.viaCookie && claims.Sid != "") {
		return info, false, unauthorized(errTokenExpired.Error())
	}
	info.subject = claims.Sub

	if claims.Sid == "" {
		// Stateless token: only a subject-level revocation can end it early.
		if sessionStore != nil {
			rb, err := sessionStore.RevokedBefore(r.Context(), claims.Sub)
			if err != nil {
				return info, false, &authError{http.StatusInternalServerError, err.Error()}
			}
			// iat has second precision: a token from the same second as the
			// revocation counts as revoked (safe side).
			if !rb.IsZero() && claims.Iat <= rb.Unix() {
				return info, false, unauthorized("auth: token revoked")
			}
		}
		return info, false, nil
	}

	if sessionStore == nil {
		return info, false, unauthorized("auth: sessions not available")
	}
	sess, ok, err := sessionStore.GetSession(r.Context(), claims.Sid)
	if err != nil {
		return info, false, &authError{http.StatusInternalServerError, err.Error()}
	}
	t := now()
	switch {
	case !ok, sess.Subject != claims.Sub:
		return info, false, unauthorized("auth: session not found")
	case !sess.RevokedAt.IsZero():
		return info, false, unauthorized("auth: session revoked")
	case !t.Before(sess.ExpiresAt):
		return info, false, unauthorized("auth: session expired")
	case !sess.SubjectRevokedBefore.IsZero() && !sess.CreatedAt.After(sess.SubjectRevokedBefore):
		return info, false, unauthorized("auth: session revoked")
	}
	info.sessionID = sess.ID
	info.sessionExpires = sess.ExpiresAt

	// Renew cookie tokens past half their lifetime (or already expired).
	if info.viaCookie {
		renew = expired || t.Sub(time.Unix(claims.Iat, 0)) > TokenLifetime/2
	}
	return info, renew, nil
}

// sameOrigin reports whether the request's Origin header names the host the
// request was sent to (Host, or X-Forwarded-Host behind a proxy). A missing
// Origin is not same-origin.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		first, _, _ := strings.Cut(xfh, ",")
		return strings.EqualFold(u.Host, strings.TrimSpace(first))
	}
	return false
}

// requireAuth is middleware that authenticates the request (cookie or Bearer),
// renews cookie session tokens past half their lifetime, and injects the
// caller into the context.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, renew, aerr := authenticate(r)
		if aerr != nil {
			writeJSONError(w, aerr.status, aerr.msg)
			return
		}
		if renew {
			if _, err := issueSessionCookie(w, info.subject, info.sessionID, info.sessionExpires); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		ctx := context.WithValue(r.Context(), ctxKeyAuth{}, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sign(data string) string {
	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func base64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
