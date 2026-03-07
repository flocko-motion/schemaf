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
	"strings"
	"time"
)

// ctxKeySubject is the context key for the authenticated subject (user ID).
type ctxKeySubject struct{}

var signingKey []byte

// InitAuth sets the HMAC-SHA256 signing key used for JWT verification.
// Called by the schemaf app after migrations run and the key is loaded from _schemaf_config.
func InitAuth(key []byte) {
	signingKey = key
}

// Subject returns the authenticated subject (user ID) from the context.
// Returns ("", false) if the request was not authenticated.
func Subject(ctx context.Context) (string, bool) {
	sub, ok := ctx.Value(ctxKeySubject{}).(string)
	return sub, ok
}

// jwtClaims is the minimal set of claims schemaf uses.
type jwtClaims struct {
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp,omitempty"`
}

// IssueToken creates a signed JWT for the given subject. exp=0 means no expiry.
func IssueToken(sub string, exp time.Time) (string, error) {
	if len(signingKey) == 0 {
		return "", errors.New("auth: signing key not initialized")
	}
	header := base64url(mustJSON(map[string]string{"alg": "HS256", "typ": "JWT"}))
	claims := jwtClaims{Sub: sub, Iat: time.Now().Unix()}
	if !exp.IsZero() {
		claims.Exp = exp.Unix()
	}
	payload := base64url(mustJSON(claims))
	sig := sign(header + "." + payload)
	return header + "." + payload + "." + sig, nil
}

// verifyToken parses and verifies a JWT, returning the claims on success.
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
	if claims.Exp != 0 && time.Now().Unix() > claims.Exp {
		return jwtClaims{}, errors.New("auth: token expired")
	}
	return claims, nil
}

// requireAuth is middleware that validates the Bearer JWT and injects the subject into context.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeJSONError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := verifyToken(token)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeySubject{}, claims.Sub)
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
