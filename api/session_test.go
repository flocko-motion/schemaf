package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flocko-motion/schemaf/db"
)

// memStore is an in-memory SessionStore for unit tests.
type memStore struct {
	mu       sync.Mutex
	sessions map[string]db.Session
	revoked  map[string]time.Time
}

func newMemStore() *memStore {
	return &memStore{sessions: map[string]db.Session{}, revoked: map[string]time.Time{}}
}

func (m *memStore) CreateSession(_ context.Context, s db.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *memStore) GetSession(_ context.Context, id string) (db.Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	s.SubjectRevokedBefore = m.revoked[s.Subject]
	return s, ok, nil
}

func (m *memStore) RevokedBefore(_ context.Context, subject string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.revoked[subject], nil
}

func (m *memStore) RevokeSession(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok && s.RevokedAt.IsZero() {
		s.RevokedAt = at
		m.sessions[id] = s
	}
	return nil
}

func (m *memStore) RevokeSubject(_ context.Context, subject string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked[subject] = at
	for id, s := range m.sessions {
		if s.Subject == subject && s.RevokedAt.IsZero() {
			s.RevokedAt = at
			m.sessions[id] = s
		}
	}
	return nil
}

// fakeProvider accepts {"user": "<sub>"}; "mallory" is refused.
type fakeProvider struct{ name string }

func (p fakeProvider) Name() string { return p.name }
func (p fakeProvider) Exchange(_ context.Context, r *http.Request) (Identity, error) {
	var body struct{ User string }
	json.NewDecoder(r.Body).Decode(&body)
	if body.User == "mallory" {
		return Identity{}, ErrForbidden
	}
	return Identity{Subject: body.User, Claims: map[string]any{"email": body.User + "@example.com"}}, nil
}

// authEnv sets up a server with a memStore, a fake provider and a
// controllable clock. It restores globals on cleanup.
type authEnv struct {
	t     *testing.T
	srv   *httptest.Server
	store *memStore
	clock time.Time
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	e := &authEnv{t: t, store: newMemStore(), clock: time.Now()}
	InitAuth([]byte("test-signing-key-0123456789abcdef"))
	InitSessions(e.store)
	SetIdentityProviders([]IdentityProvider{fakeProvider{"fake"}})
	SetOnLogin(nil)
	now = func() time.Time { return e.clock }
	t.Cleanup(func() {
		now = time.Now
		InitSessions(nil)
		SetIdentityProviders(nil)
		SetOnLogin(nil)
	})
	e.srv = httptest.NewServer(NewMux())
	t.Cleanup(e.srv.Close)
	return e
}

type reqOpts struct {
	cookie string
	bearer string
	origin string
	body   string
}

func (e *authEnv) do(method, path string, o reqOpts) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, strings.NewReader(o.body))
	if err != nil {
		e.t.Fatal(err)
	}
	if o.cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName(), Value: o.cookie})
	}
	if o.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+o.bearer)
	}
	if o.origin != "" {
		req.Header.Set("Origin", o.origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *authEnv) login(user string) (LoginResponse, *http.Cookie) {
	e.t.Helper()
	resp := e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":"` + user + `"}`})
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("login: status %d", resp.StatusCode)
	}
	var lr LoginResponse
	json.NewDecoder(resp.Body).Decode(&lr)
	return lr, sessionCookie(resp)
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == cookieName() {
			return c
		}
	}
	return nil
}

func meSubject(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/user/me: status %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	return body["id"]
}

func TestLogin(t *testing.T) {
	e := newAuthEnv(t)

	lr, c := e.login("alice")
	if lr.Subject != "alice" || lr.Token == "" {
		t.Fatalf("login response = %+v", lr)
	}
	if c == nil {
		t.Fatal("no session cookie set")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.Secure {
		t.Errorf("cookie attributes: %+v", c)
	}
	if c.MaxAge < int(SessionLifetime.Seconds())-5 {
		t.Errorf("cookie MaxAge = %d, want ~session lifetime", c.MaxAge)
	}
	if got := meSubject(t, e.do("GET", "/api/user/me", reqOpts{cookie: c.Value})); got != "alice" {
		t.Errorf("cookie subject = %q", got)
	}
	if got := meSubject(t, e.do("GET", "/api/user/me", reqOpts{bearer: lr.Token})); got != "alice" {
		t.Errorf("bearer subject = %q", got)
	}
	if s := e.store.sessions; len(s) != 1 {
		t.Fatalf("sessions = %v", s)
	}
	for _, s := range e.store.sessions {
		if s.Provider != "fake" {
			t.Errorf("session provider = %q", s.Provider)
		}
	}

	t.Run("unknown provider is 404", func(t *testing.T) {
		resp := e.do("POST", "/api/auth/nope/login", reqOpts{body: `{}`})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status %d", resp.StatusCode)
		}
		var body map[string]string
		json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] != "provider not configured" {
			t.Errorf("error = %q", body["error"])
		}
	})

	t.Run("provider refusal is 403", func(t *testing.T) {
		resp := e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":"mallory"}`})
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d", resp.StatusCode)
		}
	})

	t.Run("OnLogin maps and refuses", func(t *testing.T) {
		var got Identity
		SetOnLogin(func(_ context.Context, id Identity) (string, error) {
			got = id
			if id.Subject == "bob" {
				return "", errors.New("database down")
			}
			if id.Subject == "carol" {
				return "", ErrForbidden
			}
			return "user-" + id.Subject, nil
		})
		defer SetOnLogin(nil)

		lr, _ := e.login("dave")
		if lr.Subject != "user-dave" {
			t.Errorf("mapped subject = %q", lr.Subject)
		}
		if got.Provider != "fake" || got.Claims["email"] != "dave@example.com" {
			t.Errorf("OnLogin identity = %+v", got)
		}
		if resp := e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":"carol"}`}); resp.StatusCode != http.StatusForbidden {
			t.Errorf("refused login: status %d", resp.StatusCode)
		}
		if resp := e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":"bob"}`}); resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("OnLogin error: status %d", resp.StatusCode)
		}
	})
}

func TestTransportPrecedence(t *testing.T) {
	e := newAuthEnv(t)
	_, aliceCookie := e.login("alice")
	bob, _ := e.login("bob")

	// Both present: Bearer wins.
	if got := meSubject(t, e.do("GET", "/api/user/me", reqOpts{cookie: aliceCookie.Value, bearer: bob.Token})); got != "bob" {
		t.Errorf("both present: subject = %q, want bob (Bearer wins)", got)
	}
	// An invalid Bearer is not rescued by a valid cookie.
	if resp := e.do("GET", "/api/user/me", reqOpts{cookie: aliceCookie.Value, bearer: "garbage"}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("invalid bearer + valid cookie: status %d, want 401", resp.StatusCode)
	}
}

func TestOriginCheck(t *testing.T) {
	e := newAuthEnv(t)
	lr, c := e.login("alice")
	self := e.srv.URL // e.g. http://127.0.0.1:54321

	cases := []struct {
		name string
		o    reqOpts
		want int
	}{
		{"cookie + same origin", reqOpts{cookie: c.Value, origin: self}, http.StatusOK},
		{"cookie + cross origin", reqOpts{cookie: c.Value, origin: "https://evil.example"}, http.StatusForbidden},
		{"cookie + missing origin", reqOpts{cookie: c.Value}, http.StatusForbidden},
		{"bearer + cross origin is exempt", reqOpts{bearer: lr.Token, origin: "https://evil.example"}, http.StatusOK},
		{"bearer + missing origin is exempt", reqOpts{bearer: lr.Token}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := e.do("POST", "/api/auth/refresh", tc.o)
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}

	t.Run("cookie GET needs no origin", func(t *testing.T) {
		if resp := e.do("GET", "/api/user/me", reqOpts{cookie: c.Value}); resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
	})

	t.Run("X-Forwarded-Host", func(t *testing.T) {
		r := httptest.NewRequest("POST", "http://backend:8000/x", nil)
		r.Header.Set("Origin", "https://app.example.com")
		r.Header.Set("X-Forwarded-Host", "app.example.com")
		if !sameOrigin(r) {
			t.Error("Origin matching X-Forwarded-Host rejected")
		}
	})
}

func TestRenewal(t *testing.T) {
	e := newAuthEnv(t)
	lr, c := e.login("alice")

	t.Run("fresh token: no renewal", func(t *testing.T) {
		resp := e.do("GET", "/api/user/me", reqOpts{cookie: c.Value})
		if sessionCookie(resp) != nil {
			t.Error("cookie renewed before half-life")
		}
	})

	t.Run("past half-life: cookie renewed, same session", func(t *testing.T) {
		e.clock = e.clock.Add(TokenLifetime/2 + time.Second)
		resp := e.do("GET", "/api/user/me", reqOpts{cookie: c.Value})
		nc := sessionCookie(resp)
		if nc == nil || nc.Value == c.Value {
			t.Fatal("cookie not renewed past half-life")
		}
		old, _ := verifyToken(c.Value)
		fresh, _ := verifyToken(nc.Value)
		if fresh.Sid != old.Sid || fresh.Iat <= old.Iat {
			t.Errorf("renewed token: sid %q→%q iat %d→%d", old.Sid, fresh.Sid, old.Iat, fresh.Iat)
		}
	})

	t.Run("bearer past half-life: not renewed", func(t *testing.T) {
		resp := e.do("GET", "/api/user/me", reqOpts{bearer: lr.Token})
		if resp.StatusCode != http.StatusOK || sessionCookie(resp) != nil {
			t.Errorf("status %d, cookie %v", resp.StatusCode, sessionCookie(resp))
		}
	})

	t.Run("expired: cookie renewed while session live, bearer 401", func(t *testing.T) {
		e.clock = e.clock.Add(time.Hour)
		if resp := e.do("GET", "/api/user/me", reqOpts{bearer: lr.Token}); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expired bearer: status %d", resp.StatusCode)
		}
		resp := e.do("GET", "/api/user/me", reqOpts{cookie: c.Value})
		if resp.StatusCode != http.StatusOK || sessionCookie(resp) == nil {
			t.Errorf("expired cookie: status %d, renewed %v", resp.StatusCode, sessionCookie(resp) != nil)
		}
	})

	t.Run("session expired: cookie rejected", func(t *testing.T) {
		e.clock = e.clock.Add(SessionLifetime)
		if resp := e.do("GET", "/api/user/me", reqOpts{cookie: c.Value}); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status %d", resp.StatusCode)
		}
	})
}

func TestRefresh(t *testing.T) {
	e := newAuthEnv(t)
	lr, _ := e.login("alice")
	e.clock = e.clock.Add(10 * time.Minute)

	resp := e.do("POST", "/api/auth/refresh", reqOpts{bearer: lr.Token})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var rr RefreshResponse
	json.NewDecoder(resp.Body).Decode(&rr)
	old, _ := verifyToken(lr.Token)
	fresh, err := verifyToken(rr.Token)
	if err != nil || fresh.Sid != old.Sid || fresh.Exp <= old.Exp {
		t.Errorf("refreshed token: %+v (err %v), old %+v", fresh, err, old)
	}
	if sessionCookie(resp) != nil {
		t.Error("bearer refresh set a cookie")
	}

	stateless, _ := IssueToken("svc", time.Time{})
	if resp := e.do("POST", "/api/auth/refresh", reqOpts{bearer: stateless}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("stateless refresh: status %d, want 400", resp.StatusCode)
	}
}

func TestLogout(t *testing.T) {
	e := newAuthEnv(t)
	lr, c := e.login("alice")

	resp := e.do("POST", "/api/auth/logout", reqOpts{cookie: c.Value, origin: e.srv.URL})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	cleared := sessionCookie(resp)
	if cleared == nil || cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Errorf("cookie not cleared: %+v", cleared)
	}
	// The session is revoked: the old cookie and the Bearer token are dead.
	if resp := e.do("GET", "/api/user/me", reqOpts{cookie: c.Value}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("cookie after logout: status %d", resp.StatusCode)
	}
	if resp := e.do("GET", "/api/user/me", reqOpts{bearer: lr.Token}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bearer after logout: status %d", resp.StatusCode)
	}

	t.Run("without session: still 204", func(t *testing.T) {
		if resp := e.do("POST", "/api/auth/logout", reqOpts{}); resp.StatusCode != http.StatusNoContent {
			t.Errorf("status %d", resp.StatusCode)
		}
	})
	t.Run("cross-origin cookie logout: 403", func(t *testing.T) {
		_, c := e.login("bob")
		if resp := e.do("POST", "/api/auth/logout", reqOpts{cookie: c.Value, origin: "https://evil.example"}); resp.StatusCode != http.StatusForbidden {
			t.Errorf("status %d", resp.StatusCode)
		}
	})
}

func TestRevocation(t *testing.T) {
	e := newAuthEnv(t)
	a1, _ := e.login("alice")
	a2, _ := e.login("alice")
	b, _ := e.login("bob")
	stateless, _ := IssueToken("alice", time.Time{})

	// RevokeSession ends exactly that session.
	claims, _ := verifyToken(a1.Token)
	if err := RevokeSession(context.Background(), claims.Sid); err != nil {
		t.Fatal(err)
	}
	if resp := e.do("GET", "/api/user/me", reqOpts{bearer: a1.Token}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoked session: status %d", resp.StatusCode)
	}
	if resp := e.do("GET", "/api/user/me", reqOpts{bearer: a2.Token}); resp.StatusCode != http.StatusOK {
		t.Errorf("other session: status %d", resp.StatusCode)
	}

	// RevokeSubject ends all of alice's sessions and stateless tokens, not bob's.
	e.clock = e.clock.Add(2 * time.Second)
	if err := RevokeSubject(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string]string{"session": a2.Token, "stateless": stateless} {
		if resp := e.do("GET", "/api/user/me", reqOpts{bearer: tok}); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("alice %s after RevokeSubject: status %d", name, resp.StatusCode)
		}
	}
	if resp := e.do("GET", "/api/user/me", reqOpts{bearer: b.Token}); resp.StatusCode != http.StatusOK {
		t.Errorf("bob after alice revoked: status %d", resp.StatusCode)
	}

	// Logging in again afterwards works.
	e.clock = e.clock.Add(2 * time.Second)
	again, _ := e.login("alice")
	if resp := e.do("GET", "/api/user/me", reqOpts{bearer: again.Token}); resp.StatusCode != http.StatusOK {
		t.Errorf("new login after RevokeSubject: status %d", resp.StatusCode)
	}
	newStateless, _ := IssueToken("alice", time.Time{})
	if resp := e.do("GET", "/api/user/me", reqOpts{bearer: newStateless}); resp.StatusCode != http.StatusOK {
		t.Errorf("new stateless token after RevokeSubject: status %d", resp.StatusCode)
	}
}

func TestStartSession(t *testing.T) {
	e := newAuthEnv(t)
	w := httptest.NewRecorder()
	tok, err := StartSession(context.Background(), w, "guest-1")
	if err != nil {
		t.Fatal(err)
	}
	c := sessionCookie(&http.Response{Header: w.Header()})
	if c == nil || c.Value != tok {
		t.Fatalf("cookie = %+v", c)
	}
	for _, s := range e.store.sessions {
		if s.Provider != "" {
			t.Errorf("StartSession provider = %q, want empty", s.Provider)
		}
	}
	if got := meSubject(t, e.do("GET", "/api/user/me", reqOpts{bearer: tok})); got != "guest-1" {
		t.Errorf("subject = %q", got)
	}

	InitSessions(nil)
	if _, err := StartSession(context.Background(), httptest.NewRecorder(), "x"); err == nil {
		t.Error("StartSession without a store succeeded")
	}
}

func TestStatusProviders(t *testing.T) {
	newAuthEnv(t)
	get := func() string {
		t.Helper()
		rec := httptest.NewRecorder()
		StatusHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
		var body struct {
			Auth struct {
				Providers json.RawMessage `json:"providers"`
			} `json:"auth"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return string(body.Auth.Providers)
	}

	SetIdentityProviders(nil)
	if p := get(); p != "[]" {
		t.Errorf("no providers: got %s, want []", p)
	}

	SetIdentityProviders([]IdentityProvider{fakeProvider{"auth0"}, fakeProvider{"passkey"}})
	if p := get(); p != `["auth0","passkey"]` {
		t.Errorf("providers: got %s, want [\"auth0\",\"passkey\"]", p)
	}
}
