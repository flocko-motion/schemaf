// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// unauthEndpoint (AuthRequired) answers every request with ErrUnauthorized.
type unauthEndpoint struct{}

func (unauthEndpoint) Method() string  { return "GET" }
func (unauthEndpoint) Path() string    { return "/test/unauth" }
func (unauthEndpoint) Auth() AuthLevel { return AuthRequired }
func (unauthEndpoint) Handle(context.Context, struct{}) (struct{}, error) {
	return struct{}{}, ErrUnauthorized
}

type bodyReq struct {
	Name  string `json:"name"`
	Limit int    `query:"limit"`
}

// bodyEndpoint (AuthNone) decodes a JSON body and a query param.
type bodyEndpoint struct{}

func (bodyEndpoint) Method() string  { return "POST" }
func (bodyEndpoint) Path() string    { return "/test/body" }
func (bodyEndpoint) Auth() AuthLevel { return AuthNone }
func (bodyEndpoint) Handle(context.Context, bodyReq) (struct{}, error) {
	return struct{}{}, nil
}

func newErrorEnv(t *testing.T) *authEnv {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	Register(NewRoute[struct{}, struct{}](unauthEndpoint{}, "", ""))
	Register(NewRoute[bodyReq, struct{}](bodyEndpoint{}, "", ""))
	return newAuthEnv(t)
}

// checkErr asserts status, code and message of an error response. An empty
// msg skips the message check.
func checkErr(t *testing.T, name string, resp *http.Response, status int, code, msg string) errBody {
	t.Helper()
	if resp.StatusCode != status {
		t.Errorf("%s: status %d, want %d", name, resp.StatusCode, status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s: Content-Type = %q", name, ct)
	}
	body := decodeErrBody(t, resp)
	if body.Code != code || (msg != "" && body.Error != msg) {
		t.Errorf("%s: body %+v, want code %q msg %q", name, body, code, msg)
	}
	if (status >= 500) != (len(body.ID) == 16) {
		t.Errorf("%s: id = %q", name, body.ID)
	}
	return body
}

func TestAuthMiddleware401SaysOnlyUnauthorized(t *testing.T) {
	e := newErrorEnv(t)
	lr, _ := e.login("alice")
	expired, _ := IssueToken("alice", e.clock.Add(-time.Minute))

	cases := []struct {
		name   string
		opts   reqOpts
		reason string
	}{
		{"no credentials", reqOpts{}, "missing credentials"},
		{"malformed token", reqOpts{bearer: "garbage"}, "malformed"},
		{"bad signature", reqOpts{bearer: lr.Token[:len(lr.Token)-2] + "xx"}, "signature"},
		{"expired bearer", reqOpts{bearer: expired}, "expired"},
	}
	for _, c := range cases {
		buf := captureLog(t)
		resp := e.do("GET", "/api/user/me", c.opts)
		checkErr(t, c.name, resp, 401, "unauthorized", "Unauthorized")
		entries := logEntries(t, buf)
		if len(entries) != 1 || entries[0]["level"] != "WARN" {
			t.Errorf("%s: log = %s, want one warn entry", c.name, buf)
			continue
		}
		if r, _ := entries[0]["reason"].(string); !strings.Contains(r, c.reason) {
			t.Errorf("%s: logged reason %q, want it to mention %q", c.name, r, c.reason)
		}
	}

	// A revoked session, from the cookie: 401, cookie cleared, reason logged.
	_, c := e.login("bob")
	sid, _ := verifyToken(c.Value)
	e.store.RevokeSession(context.Background(), sid.Sid, e.clock)
	buf := captureLog(t)
	resp := e.do("GET", "/api/user/me", reqOpts{cookie: c.Value})
	checkErr(t, "revoked session", resp, 401, "unauthorized", "Unauthorized")
	if !cookieCleared(resp) {
		t.Error("revoked session: cookie not cleared")
	}
	if !strings.Contains(buf.String(), "session revoked") {
		t.Errorf("revoked session: reason not logged: %s", buf)
	}
}

func TestErrUnauthorizedLeavesCookieAlone(t *testing.T) {
	e := newErrorEnv(t)
	_, c := e.login("alice")
	resp := e.do("GET", "/test/unauth", reqOpts{cookie: c.Value})
	checkErr(t, "ErrUnauthorized", resp, 401, "unauthorized", "unauthorized")
	if sessionCookie(resp) != nil {
		t.Error("ErrUnauthorized touched the session cookie")
	}
	if h := resp.Header.Get("WWW-Authenticate"); h != "" {
		t.Errorf("WWW-Authenticate = %q", h)
	}
}

func TestFrameworkErrorsShareTheShape(t *testing.T) {
	e := newErrorEnv(t)
	_, c := e.login("alice")

	resp := e.do("POST", "/test/body", reqOpts{body: `{"name":`})
	body := checkErr(t, "bad body", resp, 400, "bad_request", "")
	if !strings.HasPrefix(body.Error, "invalid request body: ") {
		t.Errorf("bad body message = %q", body.Error)
	}

	resp = e.do("POST", "/test/body?limit=many", reqOpts{body: `{}`})
	body = checkErr(t, "bad query", resp, 400, "bad_request", "")
	if !strings.Contains(body.Error, `query param "limit"`) {
		t.Errorf("bad query message = %q", body.Error)
	}

	resp = e.do("POST", "/api/auth/refresh", reqOpts{cookie: c.Value, origin: "https://evil.example"})
	checkErr(t, "CSRF", resp, 403, "forbidden", "cross-origin request rejected: Origin header missing or not matching the host")

	resp = e.do("POST", "/api/auth/logout", reqOpts{cookie: c.Value, origin: "https://evil.example"})
	checkErr(t, "logout CSRF", resp, 403, "forbidden", "cross-origin request rejected: Origin header missing or not matching the host")

	stateless, _ := IssueToken("svc", time.Time{})
	resp = e.do("POST", "/api/auth/refresh", reqOpts{bearer: stateless})
	checkErr(t, "stateless refresh", resp, 400, "bad_request", "token is not tied to a session")

	resp = e.do("POST", "/api/auth/fake/login", reqOpts{origin: "https://evil.example", body: `{"user":"alice"}`})
	checkErr(t, "login CSRF", resp, 403, "forbidden", "cross-origin login rejected: Origin header not matching the host")

	resp = e.do("POST", "/api/auth/nope/login", reqOpts{body: `{}`})
	checkErr(t, "unknown provider", resp, 404, "not_found", "provider not configured")

	resp = e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":"mallory"}`})
	checkErr(t, "Exchange refusal", resp, 403, "forbidden", "forbidden")

	resp = e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":""}`})
	checkErr(t, "empty subject from Exchange", resp, 500, "internal", "Internal Server Error")

	SetOnLogin(func(_ context.Context, id Identity) (string, error) {
		switch id.Subject {
		case "carol":
			return "", Error(ErrForbidden, "not_invited", "you need an invitation")
		case "bob":
			return "", errors.New("pq: connection refused")
		case "eve":
			return "", nil
		}
		return id.Subject, nil
	})
	t.Cleanup(func() { SetOnLogin(nil) })
	resp = e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":"carol"}`})
	checkErr(t, "OnLogin coded refusal", resp, 403, "not_invited", "you need an invitation")
	buf := captureLog(t)
	resp = e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":"bob"}`})
	checkErr(t, "OnLogin failure", resp, 500, "internal", "Internal Server Error")
	if !strings.Contains(buf.String(), "pq: connection refused") {
		t.Errorf("OnLogin failure not logged: %s", buf)
	}
	resp = e.do("POST", "/api/auth/fake/login", reqOpts{body: `{"user":"eve"}`})
	checkErr(t, "empty subject from OnLogin", resp, 500, "internal", "Internal Server Error")
}
