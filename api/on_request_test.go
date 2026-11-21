// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ctxKeyCaller is the test app's own caller key, set by the hook.
type ctxKeyCaller struct{}

func callerFrom(ctx context.Context) string {
	c, _ := ctx.Value(ctxKeyCaller{}).(string)
	return c
}

type callerResp struct {
	Caller  string `json:"caller"`
	Subject string `json:"subject"`
}

// callerEndpoint reports the caller the hook put into ctx.
type callerEndpoint struct{ level AuthLevel }

func (e callerEndpoint) Method() string  { return "GET" }
func (e callerEndpoint) Path() string    { return "/api/hook/" + e.level.String() }
func (e callerEndpoint) Auth() AuthLevel { return e.level }
func (e callerEndpoint) Handle(ctx context.Context, _ struct{}) (callerResp, error) {
	sub, _ := Subject(ctx)
	return callerResp{Caller: callerFrom(ctx), Subject: sub}, nil
}

type callerRawEndpoint struct{ level AuthLevel }

func (e callerRawEndpoint) Method() string  { return "GET" }
func (e callerRawEndpoint) Path() string    { return "/api/hook/raw/" + e.level.String() }
func (e callerRawEndpoint) Auth() AuthLevel { return e.level }
func (e callerRawEndpoint) HandleRaw(w http.ResponseWriter, r *http.Request) error {
	sub, _ := Subject(r.Context())
	writeJSON(w, callerResp{Caller: callerFrom(r.Context()), Subject: sub})
	return nil
}

type callerStreamEndpoint struct{ level AuthLevel }

func (e callerStreamEndpoint) Method() string  { return "GET" }
func (e callerStreamEndpoint) Path() string    { return "/api/hook/stream/" + e.level.String() }
func (e callerStreamEndpoint) Auth() AuthLevel { return e.level }
func (e callerStreamEndpoint) Stream(ctx context.Context, _ struct{}) (<-chan callerResp, error) {
	sub, _ := Subject(ctx)
	ch := make(chan callerResp, 1)
	ch <- callerResp{Caller: callerFrom(ctx), Subject: sub}
	close(ch)
	return ch, nil
}

// hookCall records the caller the admission reported in one call of the test hook.
type hookCall struct {
	subject string
	ok      bool
}

// hookRecorder is a test OnRequest hook: it records its calls, enriches ctx
// with "caller:<subject>" (or "caller:anon"), and refuses with refuse if set.
type hookRecorder struct {
	mu     sync.Mutex
	calls  []hookCall
	refuse error
	nilCtx bool
}

func (h *hookRecorder) hook(ctx context.Context, a *Admission) (context.Context, error) {
	subject, ok := a.Subject()
	// The hook's ctx reports the same caller as its admission.
	if cs, cok := Subject(ctx); cs != subject || cok != ok {
		return nil, fmt.Errorf("admission says (%q, %v), ctx says (%q, %v)", subject, ok, cs, cok)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, hookCall{subject: subject, ok: ok})
	if h.refuse != nil {
		return nil, h.refuse
	}
	if h.nilCtx {
		return nil, nil
	}
	c := "caller:anon"
	if ok {
		c = "caller:" + subject
	}
	return context.WithValue(ctx, ctxKeyCaller{}, c), nil
}

func (h *hookRecorder) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = nil
}

func (h *hookRecorder) got() []hookCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hookCall(nil), h.calls...)
}

// newHookEnv registers typed, raw and stream caller endpoints at every level,
// starts the auth env and installs a recording OnRequest hook.
func newHookEnv(t *testing.T) (*authEnv, *hookRecorder) {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	for _, l := range allLevels {
		Register(NewRoute[struct{}, callerResp](callerEndpoint{l}, "", ""))
		Register(NewRawRoute(callerRawEndpoint{l}, "", ""))
		Register(NewStreamRoute[struct{}, callerResp](callerStreamEndpoint{l}, "", ""))
	}
	e := newAuthEnv(t)
	h := &hookRecorder{}
	SetOnRequest(h.hook)
	return e, h
}

func decodeCaller(t *testing.T, resp *http.Response, stream bool) callerResp {
	t.Helper()
	var c callerResp
	if stream {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				if err := json.Unmarshal([]byte(data), &c); err != nil {
					t.Fatalf("decode event: %v", err)
				}
				return c
			}
		}
		t.Fatalf("no event in stream: %v", sc.Err())
	}
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return c
}

func TestOnRequestEnrichesAtEveryLevel(t *testing.T) {
	e, h := newHookEnv(t)
	alice, aliceCookie := e.login("alice")

	creds := []struct {
		name string
		o    reqOpts
		sub  string // "" → anonymous
	}{
		{"no credentials", reqOpts{}, ""},
		{"cookie", reqOpts{cookie: aliceCookie.Value}, "alice"},
		{"bearer", reqOpts{bearer: alice.Token}, "alice"},
	}
	for _, kind := range []string{"", "raw/", "stream/"} {
		for _, l := range allLevels {
			for _, c := range creds {
				if l == AuthRequired && c.sub == "" {
					continue // 401: covered by TestOnRequestNotAfterFailedAuth
				}
				t.Run(kind+l.String()+"/"+c.name, func(t *testing.T) {
					// AuthNone never looks at credentials.
					wantSub, wantOK := c.sub, c.sub != ""
					if l == AuthNone {
						wantSub, wantOK = "", false
					}
					h.reset()
					resp := e.do("GET", "/api/hook/"+kind+l.String(), c.o)
					if resp.StatusCode != http.StatusOK {
						t.Fatalf("status %d", resp.StatusCode)
					}
					got := h.got()
					if len(got) != 1 || got[0].subject != wantSub || got[0].ok != wantOK {
						t.Fatalf("hook calls %+v, want one (%q, %v)", got, wantSub, wantOK)
					}
					wantCaller := "caller:anon"
					if wantOK {
						wantCaller = "caller:" + wantSub
					}
					cr := decodeCaller(t, resp, kind == "stream/")
					if cr.Caller != wantCaller {
						t.Errorf("handler saw caller %q, want %q", cr.Caller, wantCaller)
					}
					// Nothing is sealed: api.Subject keeps working after the hook.
					if cr.Subject != wantSub {
						t.Errorf("handler saw subject %q, want %q", cr.Subject, wantSub)
					}
				})
			}
		}
	}
}

func TestOnRequestRefusesAtEveryLevel(t *testing.T) {
	e, h := newHookEnv(t)
	alice, _ := e.login("alice")
	h.refuse = Error(ErrForbidden, "account_incomplete", "finish your registration first")
	for _, kind := range []string{"", "raw/", "stream/"} {
		for _, l := range allLevels {
			t.Run(kind+l.String(), func(t *testing.T) {
				resp := e.do("GET", "/api/hook/"+kind+l.String(), reqOpts{bearer: alice.Token})
				checkErr(t, "refused", resp, http.StatusForbidden, "account_incomplete", "finish your registration first")
			})
		}
	}
}

func TestOnRequestRefusalIs5xxWithID(t *testing.T) {
	e, h := newHookEnv(t)
	h.refuse = errors.New("users db down")
	resp := e.do("GET", "/api/hook/AuthNone", reqOpts{})
	checkErr(t, "refused", resp, http.StatusInternalServerError, "internal", "Internal Server Error")
}

func TestOnRequestUnauthorizedKeepsCookie(t *testing.T) {
	e, h := newHookEnv(t)
	_, cookie := e.login("alice")
	h.refuse = ErrUnauthorized
	resp := e.do("GET", "/api/hook/AuthRequired", reqOpts{cookie: cookie.Value})
	checkErr(t, "refused", resp, http.StatusUnauthorized, "unauthorized", "")
	if c := sessionCookie(resp); c != nil {
		t.Errorf("hook's ErrUnauthorized touched the cookie: %+v", c)
	}
}

func TestOnRequestRefusalKeepsCookieRenewal(t *testing.T) {
	e, h := newHookEnv(t)
	_, cookie := e.login("alice")
	e.clock = e.clock.Add(TokenLifetime/2 + time.Minute) // past half: renewal due
	h.refuse = ErrForbidden
	resp := e.do("GET", "/api/hook/AuthRequired", reqOpts{cookie: cookie.Value})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
	if c := sessionCookie(resp); c == nil || c.Value == "" || c.Value == cookie.Value {
		t.Errorf("renewed cookie missing on the refusal: %+v", c)
	}
}

func TestOnRequestNilContextIs500(t *testing.T) {
	e, h := newHookEnv(t)
	h.nilCtx = true
	resp := e.do("GET", "/api/hook/AuthNone", reqOpts{})
	body := checkErr(t, "nil ctx", resp, http.StatusInternalServerError, "internal", "")
	d, ok := errorDetails.lookup(body.ID)
	if !ok {
		t.Fatalf("no details recorded for %s", body.ID)
	}
	if !strings.Contains(d.Error, "OnRequest") {
		t.Errorf("logged error %q does not name the OnRequest hook", d.Error)
	}
}

func TestOnRequestNotAfterFailedAuth(t *testing.T) {
	e, h := newHookEnv(t)
	bob, _ := e.login("bob")
	bobClaims, _ := verifyToken(bob.Token)
	if err := RevokeSession(context.Background(), bobClaims.Sid); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path string
		o          reqOpts
		status     int
	}{
		{"required, no credentials", "/api/hook/AuthRequired", reqOpts{}, 401},
		{"required, revoked", "/api/hook/AuthRequired", reqOpts{bearer: bob.Token}, 401},
		{"optional, garbage", "/api/hook/AuthOptional", reqOpts{bearer: "not.a.jwt"}, 401},
	}
	for _, c := range cases {
		h.reset()
		resp := e.do("GET", c.path, c.o)
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.status)
		}
		if got := h.got(); len(got) != 0 {
			t.Errorf("%s: hook ran after failed auth: %+v", c.name, got)
		}
	}
}

func TestOnRequestBuiltInRoutes(t *testing.T) {
	t.Setenv("SCHEMAF_DEV", "") // production: anonymous error details are a 401
	e, h := newHookEnv(t)
	alice, _ := e.login("alice")
	cases := []struct {
		name, method, path string
		o                  reqOpts
		status             int
		sub                string
		ok                 bool
	}{
		{"health", "GET", "/api/health", reqOpts{bearer: alice.Token}, 200, "", false},
		{"status", "GET", "/api/status", reqOpts{}, 200, "", false},
		{"config", "GET", "/api/config", reqOpts{}, 200, "", false},
		{"user/me", "GET", "/api/user/me", reqOpts{bearer: alice.Token}, 200, "alice", true},
		{"refresh", "POST", "/api/auth/refresh", reqOpts{bearer: alice.Token}, 200, "alice", true},
		{"logout", "POST", "/api/auth/logout", reqOpts{}, 204, "", false},
		{"login", "POST", "/api/auth/fake/login", reqOpts{body: `{"user":"carol"}`}, 200, "", false},
		{"errors", "GET", "/api/errors/0123456789abcdef", reqOpts{}, 401, "", false},
	}
	for _, c := range cases {
		h.reset()
		resp := e.do(c.method, c.path, c.o)
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.status)
		}
		got := h.got()
		if len(got) != 1 || got[0].subject != c.sub || got[0].ok != c.ok {
			t.Errorf("%s: hook calls %+v, want one (%q, %v)", c.name, got, c.sub, c.ok)
		}
	}
}

func TestOnRequestRefusesBuiltInRoute(t *testing.T) {
	e, h := newHookEnv(t)
	h.refuse = Error(ErrForbidden, "maintenance", "down for maintenance")
	resp := e.do("GET", "/api/health", reqOpts{})
	checkErr(t, "health", resp, http.StatusForbidden, "maintenance", "down for maintenance")
}

func TestOnRequestNotOutsideTheAPI(t *testing.T) {
	_, h := newHookEnv(t)
	frontendModes(t, func(t *testing.T) {
		for _, c := range []struct{ method, path string }{
			{"GET", "/"},              // frontend
			{"GET", "/some/page"},     // frontend
			{"GET", "/openapi.json"},  // the spec
			{"GET", "/api/nope"},      // route gate: 404
			{"DELETE", "/api/health"}, // route gate: 405
		} {
			h.reset()
			serveMux(t, c.method, c.path)
			if got := h.got(); len(got) != 0 {
				t.Errorf("%s %s: hook ran: %+v", c.method, c.path, got)
			}
		}
	})
}

func TestNoOnRequestHookRunsAsBefore(t *testing.T) {
	e, _ := newHookEnv(t)
	SetOnRequest(nil)
	resp := e.do("GET", "/api/hook/AuthNone", reqOpts{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if cr := decodeCaller(t, resp, false); cr.Caller != "" {
		t.Errorf("caller %q without a hook", cr.Caller)
	}
}

// wantAdmissionPanic calls Subject on a and checks it panics naming the cause.
func wantAdmissionPanic(t *testing.T, name string, a *Admission, cause string) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		msg, _ := r.(string)
		if r == nil || !strings.Contains(msg, cause) {
			t.Errorf("%s: Subject() panic = %v, want one containing %q", name, r, cause)
		}
	}()
	a.Subject()
}

// An admission (and any copy of it) is valid only while its hook runs.
func TestAdmissionInvalidAfterHook(t *testing.T) {
	e, _ := newHookEnv(t)
	alice, _ := e.login("alice")
	var kept *Admission
	var copied Admission
	var validInHook bool
	SetOnRequest(func(ctx context.Context, a *Admission) (context.Context, error) {
		kept, copied, validInHook = a, *a, a.Valid()
		return ctx, nil
	})
	if resp := e.do("GET", "/api/hook/AuthRequired", reqOpts{bearer: alice.Token}); resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !validInHook {
		t.Error("admission not valid inside the hook")
	}
	if kept.Valid() || copied.Valid() {
		t.Errorf("after the hook: kept valid = %v, copy valid = %v, want both false", kept.Valid(), copied.Valid())
	}
	wantAdmissionPanic(t, "kept", kept, "after its OnRequest hook returned")
	wantAdmissionPanic(t, "copy", &copied, "after its OnRequest hook returned")
}

// An admission the app makes itself is never valid.
func TestAdmissionForgedIsInvalid(t *testing.T) {
	var nilAdm *Admission
	if nilAdm.Valid() || (&Admission{}).Valid() {
		t.Error("a nil or zero admission reports valid")
	}
	wantAdmissionPanic(t, "nil", nilAdm, "nil")
	wantAdmissionPanic(t, "zero", &Admission{}, "not issued by schemaf")
}

// A hook that panics still invalidates its admission.
func TestAdmissionInvalidAfterHookPanics(t *testing.T) {
	e, _ := newHookEnv(t)
	var kept *Admission
	SetOnRequest(func(ctx context.Context, a *Admission) (context.Context, error) {
		kept = a
		panic("hook bug")
	})
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/hook/AuthNone", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
	if kept == nil || kept.Valid() {
		t.Errorf("admission after a panicking hook: %v, want invalid", kept)
	}
}
