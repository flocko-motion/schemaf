// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"
)

type whoReq struct{}

type whoResp struct {
	Subject string `json:"subject"`
	Signed  bool   `json:"signed"`
}

// whoEndpoint reports the caller api.Subject sees, at a given level.
type whoEndpoint struct {
	method string
	level  AuthLevel
}

func (e whoEndpoint) Method() string  { return e.method }
func (e whoEndpoint) Path() string    { return "/test/" + e.level.String() }
func (e whoEndpoint) Auth() AuthLevel { return e.level }
func (e whoEndpoint) Handle(ctx context.Context, _ whoReq) (whoResp, error) {
	sub, ok := Subject(ctx)
	return whoResp{Subject: sub, Signed: ok}, nil
}

// whoRawEndpoint is the raw-endpoint twin of whoEndpoint.
type whoRawEndpoint struct{ level AuthLevel }

func (e whoRawEndpoint) Method() string  { return "GET" }
func (e whoRawEndpoint) Path() string    { return "/test/raw/" + e.level.String() }
func (e whoRawEndpoint) Auth() AuthLevel { return e.level }
func (e whoRawEndpoint) HandleRaw(w http.ResponseWriter, r *http.Request) error {
	sub, ok := Subject(r.Context())
	writeJSON(w, whoResp{Subject: sub, Signed: ok})
	return nil
}

var allLevels = []AuthLevel{AuthNone, AuthOptional, AuthRequired}

// newLevelEnv registers GET and POST test endpoints (typed and raw) at every
// level, then starts the auth env.
func newLevelEnv(t *testing.T) *authEnv {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	for _, l := range allLevels {
		Register(NewRoute[whoReq, whoResp](whoEndpoint{"GET", l}, "", ""))
		Register(NewRoute[whoReq, whoResp](whoEndpoint{"POST", l}, "", ""))
		Register(NewRawRoute(whoRawEndpoint{l}, "", ""))
	}
	return newAuthEnv(t)
}

func decodeWho(t *testing.T, resp *http.Response) whoResp {
	t.Helper()
	var w whoResp
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return w
}

func cookieCleared(resp *http.Response) bool {
	c := sessionCookie(resp)
	return c != nil && c.Value == "" && c.MaxAge < 0
}

func TestAuthLevels(t *testing.T) {
	e := newLevelEnv(t)
	alice, aliceCookie := e.login("alice")
	bob, bobCookie := e.login("bob")
	bobClaims, _ := verifyToken(bob.Token)
	if err := RevokeSession(context.Background(), bobClaims.Sid); err != nil {
		t.Fatal(err)
	}

	type want struct {
		status  int
		subject string // "" → anonymous (Subject returns ok=false)
		cleared bool
	}
	creds := []struct {
		name string
		o    reqOpts
	}{
		{"no credentials", reqOpts{}},
		{"valid cookie", reqOpts{cookie: aliceCookie.Value}},
		{"valid bearer", reqOpts{bearer: alice.Token}},
		{"invalid cookie", reqOpts{cookie: bobCookie.Value}},
		{"invalid bearer", reqOpts{bearer: bob.Token}},
		{"garbage bearer", reqOpts{bearer: "not.a.jwt"}},
	}
	wants := map[AuthLevel][]want{
		AuthNone: {
			{200, "", false}, {200, "", false}, {200, "", false},
			{200, "", false}, {200, "", false}, {200, "", false},
		},
		AuthOptional: {
			{200, "", false}, {200, "alice", false}, {200, "alice", false},
			{401, "", true}, {401, "", false}, {401, "", false},
		},
		AuthRequired: {
			{401, "", false}, {200, "alice", false}, {200, "alice", false},
			{401, "", true}, {401, "", false}, {401, "", false},
		},
	}

	for _, prefix := range []string{"/test/", "/test/raw/"} {
		for _, l := range allLevels {
			for i, c := range creds {
				w := wants[l][i]
				t.Run(prefix+l.String()+"/"+c.name, func(t *testing.T) {
					resp := e.do("GET", prefix+l.String(), c.o)
					if resp.StatusCode != w.status {
						t.Fatalf("status %d, want %d", resp.StatusCode, w.status)
					}
					if got := cookieCleared(resp); got != w.cleared {
						t.Errorf("cookie cleared = %v, want %v", got, w.cleared)
					}
					if w.status != 200 {
						return
					}
					who := decodeWho(t, resp)
					if who.Subject != w.subject || who.Signed != (w.subject != "") {
						t.Errorf("Subject = (%q, %v), want %q", who.Subject, who.Signed, w.subject)
					}
				})
			}
		}
	}
}

func TestAuthOptionalRenewal(t *testing.T) {
	e := newLevelEnv(t)
	_, c := e.login("alice")

	if resp := e.do("GET", "/test/AuthOptional", reqOpts{cookie: c.Value}); sessionCookie(resp) != nil {
		t.Error("cookie renewed before half-life")
	}
	e.clock = e.clock.Add(TokenLifetime/2 + time.Second)
	resp := e.do("GET", "/test/AuthOptional", reqOpts{cookie: c.Value})
	nc := sessionCookie(resp)
	if resp.StatusCode != 200 || nc == nil || nc.Value == "" || nc.Value == c.Value {
		t.Fatalf("status %d, cookie not renewed past half-life", resp.StatusCode)
	}
	// Expired cookie token for a live session: still valid, renewed.
	e.clock = e.clock.Add(time.Hour)
	resp = e.do("GET", "/test/AuthOptional", reqOpts{cookie: c.Value})
	if resp.StatusCode != 200 || sessionCookie(resp) == nil || decodeWho(t, resp).Subject != "alice" {
		t.Fatalf("expired cookie, live session: status %d", resp.StatusCode)
	}
}

func TestAuthOptionalOriginCheck(t *testing.T) {
	e := newLevelEnv(t)
	lr, c := e.login("alice")
	cases := []struct {
		name    string
		o       reqOpts
		status  int
		subject string
	}{
		{"cookie + same origin", reqOpts{cookie: c.Value, origin: e.srv.URL}, 200, "alice"},
		{"cookie + cross origin", reqOpts{cookie: c.Value, origin: "https://evil.example"}, 403, ""},
		{"cookie + missing origin", reqOpts{cookie: c.Value}, 403, ""},
		{"bearer + cross origin is exempt", reqOpts{bearer: lr.Token, origin: "https://evil.example"}, 200, "alice"},
		{"no credentials + cross origin", reqOpts{origin: "https://evil.example"}, 200, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := e.do("POST", "/test/AuthOptional", tc.o)
			if resp.StatusCode != tc.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.status)
			}
			if cookieCleared(resp) {
				t.Error("cookie cleared on a valid credential")
			}
			if tc.status == 200 {
				if who := decodeWho(t, resp); who.Subject != tc.subject {
					t.Errorf("subject %q, want %q", who.Subject, tc.subject)
				}
			}
		})
	}
}

func TestFrameworkRouteLevels(t *testing.T) {
	got := map[string]AuthLevel{}
	for _, fr := range frameworkRoutes() {
		got[fr.pattern] = fr.auth
	}
	want := map[string]AuthLevel{
		"GET /openapi.json":               AuthNone,
		"GET /api/health":                 AuthNone,
		"GET /api/status":                 AuthNone,
		"GET /api/config":                 AuthNone,
		"GET /api/user/me":                AuthRequired,
		"POST /api/auth/{provider}/login": AuthNone,
		"POST /api/auth/logout":           AuthNone,
		"POST /api/auth/refresh":          AuthRequired,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("framework route levels:\n got %v\nwant %v", got, want)
	}

	// Behaviour, not just the table: an invalid cookie on an AuthNone route
	// is ignored, on an AuthRequired route it is a 401 that clears it.
	e := newAuthEnv(t)
	if resp := e.do("GET", "/api/health", reqOpts{cookie: "garbage"}); resp.StatusCode == 401 || cookieCleared(resp) {
		t.Errorf("/api/health with a bad cookie: status %d", resp.StatusCode)
	}
	if resp := e.do("GET", "/api/user/me", reqOpts{cookie: "garbage"}); resp.StatusCode != 401 || !cookieCleared(resp) {
		t.Errorf("/api/user/me with a bad cookie: status %d, cleared %v", resp.StatusCode, cookieCleared(resp))
	}
}

func TestOpenAPISecurityPerLevel(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	for _, l := range allLevels {
		Register(NewRoute[whoReq, whoResp](whoEndpoint{"GET", l}, "", ""))
	}
	raw, err := json.Marshal(BuildOpenAPI("API", "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths      map[string]map[string]map[string]any `json:"paths"`
		Components struct {
			SecuritySchemes map[string]map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	checkOpenAPISecurity(t, spec.Paths, spec.Components.SecuritySchemes, cookieName(), map[string]AuthLevel{
		"/test/AuthNone":     AuthNone,
		"/test/AuthOptional": AuthOptional,
		"/test/AuthRequired": AuthRequired,
	})
}

// checkOpenAPISecurity asserts the security schemes and the per-level
// security of GET paths, plus POST /api/auth/refresh as AuthRequired.
func checkOpenAPISecurity(t *testing.T, paths map[string]map[string]map[string]any, schemes map[string]map[string]any, cookie string, levels map[string]AuthLevel) {
	t.Helper()
	if s := schemes["cookieAuth"]; s["type"] != "apiKey" || s["in"] != "cookie" || s["name"] != cookie {
		t.Errorf("cookieAuth scheme = %v", s)
	}
	if s := schemes["bearerAuth"]; s["type"] != "http" || s["scheme"] != "bearer" {
		t.Errorf("bearerAuth scheme = %v", s)
	}
	wantSec := map[AuthLevel]string{
		AuthNone:     "null",
		AuthOptional: `[{},{"bearerAuth":[]},{"cookieAuth":[]}]`,
		AuthRequired: `[{"bearerAuth":[]},{"cookieAuth":[]}]`,
	}
	check := func(path, method string, l AuthLevel) {
		op := paths[path][method]
		if op == nil {
			t.Errorf("%s %s missing", method, path)
			return
		}
		sec, _ := json.Marshal(op["security"])
		if string(sec) != wantSec[l] {
			t.Errorf("%s %s security = %s, want %s", method, path, sec, wantSec[l])
		}
		_, has401 := op["responses"].(map[string]any)["401"]
		if has401 != (l != AuthNone) {
			t.Errorf("%s %s: 401 response present = %v", method, path, has401)
		}
	}
	for p, l := range levels {
		check(p, "get", l)
	}
	check("/api/auth/refresh", "post", AuthRequired)
}
