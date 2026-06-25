// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRequireAuth exercises the real HTTP auth path (requireAuth + Subject) via
// the always-present /api/user/me endpoint, using an in-process httptest server.
// This is the mechanism the `auth token` CLI relies on.
func TestRequireAuth(t *testing.T) {
	InitAuth([]byte("test-signing-key-0123456789abcdef"))
	srv := httptest.NewServer(NewMux())
	defer srv.Close()

	get := func(bearer string) *http.Response {
		t.Helper()
		req, err := http.NewRequest("GET", srv.URL+"/api/user/me", nil)
		if err != nil {
			t.Fatal(err)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	t.Run("no token is rejected", func(t *testing.T) {
		resp := get("")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", resp.StatusCode)
		}
	})

	t.Run("malformed token is rejected", func(t *testing.T) {
		resp := get("not.a.jwt")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", resp.StatusCode)
		}
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		tok, err := IssueToken("bob", time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		resp := get(tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", resp.StatusCode)
		}
	})

	t.Run("valid token authenticates and carries the subject", func(t *testing.T) {
		tok, err := IssueToken("alice", time.Time{}) // no expiry
		if err != nil {
			t.Fatal(err)
		}
		resp := get(tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d, want 200", resp.StatusCode)
		}
		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["id"] != "alice" {
			t.Fatalf("subject = %q, want alice", body["id"])
		}
	})
}

// TestIssueTokenRoundTrip covers the exact primitive behind `auth token`: a token
// it mints verifies and carries the right subject. A token with a different
// signing key must be rejected.
func TestIssueTokenRoundTrip(t *testing.T) {
	InitAuth([]byte("test-signing-key-0123456789abcdef"))
	tok, err := IssueToken("carol", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifyToken(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Sub != "carol" {
		t.Fatalf("sub = %q, want carol", claims.Sub)
	}

	// A token signed with a different key must not verify.
	InitAuth([]byte("a-completely-different-signing-key"))
	if _, err := verifyToken(tok); err == nil {
		t.Fatal("token verified under a different signing key — signature not checked")
	}
}
