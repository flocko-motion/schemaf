// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// errAuthRawEndpoint is a raw endpoint at AuthRequired returning a fixed error.
type errAuthRawEndpoint struct{ err error }

func (e errAuthRawEndpoint) Method() string                                     { return "GET" }
func (e errAuthRawEndpoint) Path() string                                       { return "/api/test/raw/autherr" }
func (e errAuthRawEndpoint) Auth() AuthLevel                                    { return AuthRequired }
func (e errAuthRawEndpoint) HandleRaw(http.ResponseWriter, *http.Request) error { return e.err }

// TestErrorDetailsRecorded: a 5xx from an endpoint (authenticated, misuse)
// and from a bare handler calling WriteError are both stored with every
// field filled; a 4xx is not stored.
func TestErrorDetailsRecorded(t *testing.T) {
	captureLog(t)

	// From an endpoint: subject and misuse present.
	Reset()
	t.Cleanup(Reset)
	Register(NewRawRoute(errAuthRawEndpoint{Error(ErrConflict, "not_found", "x")}, "", ""))
	e := newAuthEnv(t)
	tok, err := IssueToken("alice", e.clock.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	resp := e.do("GET", "/api/test/raw/autherr", reqOpts{bearer: tok})
	body := decodeErrBody(t, resp)
	if resp.StatusCode != 500 || body.ID == "" {
		t.Fatalf("got %d %+v", resp.StatusCode, body)
	}
	d, ok := LookupError(body.ID)
	if !ok {
		t.Fatalf("LookupError(%s): not found", body.ID)
	}
	want := ErrorDetails{
		ID: body.ID, Time: d.Time, Method: "GET", Path: "/api/test/raw/autherr",
		Status: 500, Code: "internal", Subject: "alice", Error: "x", Misuse: d.Misuse,
	}
	if d != want || d.Time.IsZero() || !strings.Contains(d.Misuse, "default code") {
		t.Errorf("details = %+v", d)
	}

	// From a bare handler: everything but subject (no auth context).
	resp2 := writeErrorDirect(t, fmt.Errorf("upstream: %w", ErrBadGateway))
	var b2 errorResponse
	json.NewDecoder(resp2.Body).Decode(&b2)
	d2, ok := LookupError(b2.ID)
	if !ok {
		t.Fatalf("LookupError(%s) from a bare handler: not found", b2.ID)
	}
	if d2.Method != "GET" || d2.Path != "/standalone/err" || d2.Status != 502 || d2.Code != "bad_gateway" ||
		d2.Error != "upstream: bad gateway" || d2.Time.IsZero() || d2.Subject != "" || d2.Misuse != "" {
		t.Errorf("details = %+v", d2)
	}

	// A 4xx has no id and is not stored.
	before := errorDetails.len()
	writeErrorDirect(t, ErrConflict)
	if after := errorDetails.len(); after != before {
		t.Errorf("4xx stored: %d → %d entries", before, after)
	}
}

// TestErrorDetailsEviction: the store keeps the newest errorDetailsMax
// entries, each for at most errorDetailsTTL.
func TestErrorDetailsEviction(t *testing.T) {
	clock := time.Now()
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })
	s := newErrorStore()

	for i := 0; i <= errorDetailsMax; i++ {
		s.record(ErrorDetails{ID: fmt.Sprintf("id%d", i), Time: now()})
	}
	if _, ok := s.lookup("id0"); ok {
		t.Error("oldest entry kept beyond the count limit")
	}
	if _, ok := s.lookup("id1"); !ok {
		t.Error("second-oldest entry evicted")
	}
	if n := s.len(); n != errorDetailsMax {
		t.Errorf("%d entries, want %d", n, errorDetailsMax)
	}

	clock = clock.Add(errorDetailsTTL / 2)
	s.record(ErrorDetails{ID: "fresh", Time: now()})
	clock = clock.Add(errorDetailsTTL/2 + time.Second)
	if _, ok := s.lookup("id1"); ok {
		t.Error("entry kept beyond the age limit")
	}
	if _, ok := s.lookup("fresh"); !ok {
		t.Error("young entry evicted")
	}
	s.record(ErrorDetails{ID: "another", Time: now()})
	if n := s.len(); n != 2 {
		t.Errorf("%d entries after expiry, want 2", n)
	}
}

// detailsEnv serves the framework routes with a stored error and returns
// the env and the error's id.
func detailsEnv(t *testing.T, dev bool) (*authEnv, string) {
	t.Helper()
	if dev {
		t.Setenv("SCHEMAF_DEV", "1")
	} else {
		t.Setenv("SCHEMAF_DEV", "")
	}
	captureLog(t)
	Reset()
	t.Cleanup(Reset)
	e := newAuthEnv(t)
	resp := writeErrorDirect(t, errors.New("db: disk full"))
	var b errorResponse
	json.NewDecoder(resp.Body).Decode(&b)
	return e, b.ID
}

func bearerFor(t *testing.T, e *authEnv, sub string) string {
	t.Helper()
	tok, err := IssueToken(sub, e.clock.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestErrorDetailsEndpointDevMode(t *testing.T) {
	e, id := detailsEnv(t, true)
	// A predicate is ignored in dev mode.
	SetCanSeeErrorDetails(func(context.Context, string) bool { return false })
	resp := e.do("GET", "/api/errors/"+id, reqOpts{})
	if resp.StatusCode != 200 {
		t.Fatalf("anonymous in dev mode: status %d", resp.StatusCode)
	}
	var d ErrorDetails
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatal(err)
	}
	if d.ID != id || d.Error != "db: disk full" || d.Status != 500 || d.Code != "internal" {
		t.Errorf("details = %+v", d)
	}
	if resp := e.do("GET", "/api/errors/0000000000000000", reqOpts{}); resp.StatusCode != 404 || decodeErrBody(t, resp).Code != "not_found" {
		t.Errorf("unknown id: status %d", resp.StatusCode)
	}
}

func TestErrorDetailsEndpointProduction(t *testing.T) {
	e, id := detailsEnv(t, false)
	path := "/api/errors/" + id
	check := func(name string, o reqOpts, status int, code string) {
		t.Helper()
		resp := e.do("GET", path, o)
		if resp.StatusCode != status {
			t.Errorf("%s: status %d, want %d", name, resp.StatusCode, status)
			return
		}
		if code != "" {
			if b := decodeErrBody(t, resp); b.Code != code {
				t.Errorf("%s: code %q, want %q", name, b.Code, code)
			}
		}
	}

	check("no predicate, signed in", reqOpts{bearer: bearerFor(t, e, "admin")}, 403, "forbidden")

	SetCanSeeErrorDetails(func(_ context.Context, sub string) bool { return sub == "admin" })
	check("anonymous", reqOpts{}, 401, "unauthorized")
	if b := decodeErrBody(t, e.do("GET", path, reqOpts{})); b.Error != "Unauthorized" {
		t.Errorf("anonymous: message %q, want only \"Unauthorized\" (the reason goes to the log)", b.Error)
	}
	check("predicate false", reqOpts{bearer: bearerFor(t, e, "bob")}, 403, "forbidden")
	check("predicate true", reqOpts{bearer: bearerFor(t, e, "admin")}, 200, "")
	if resp := e.do("GET", "/api/errors/ffffffffffffffff", reqOpts{bearer: bearerFor(t, e, "admin")}); resp.StatusCode != 404 {
		t.Errorf("unknown id: status %d, want 404", resp.StatusCode)
	}

	// Reset clears the predicate: details are off again.
	Reset()
	if canSeeErrorDetails != nil {
		t.Error("Reset kept the CanSeeErrorDetails predicate")
	}
}

// TestErrorDetailsInSpec: the route and its schema are in the framework's
// part of the spec, at AuthOptional.
func TestErrorDetailsInSpec(t *testing.T) {
	paths, schemas := FrameworkOpenAPI()
	item, _ := paths["/api/errors/{id}"].(map[string]any)
	op, _ := item["get"].(map[string]any)
	if op == nil {
		t.Fatal("FrameworkOpenAPI has no GET /api/errors/{id}")
	}
	sec, _ := json.Marshal(op["security"])
	want, _ := json.Marshal(OpenAPISecurity(AuthOptional))
	if string(sec) != string(want) {
		t.Errorf("security = %s, want %s", sec, want)
	}
	b, _ := json.Marshal(op)
	if !strings.Contains(string(b), "#/components/schemas/SchemafErrorDetails") {
		t.Errorf("operation does not reference SchemafErrorDetails: %s", b)
	}
	s, _ := schemas["SchemafErrorDetails"].(map[string]any)
	props, _ := s["properties"].(map[string]any)
	for _, f := range []string{"id", "time", "method", "path", "status", "code", "subject", "error", "misuse"} {
		if _, ok := props[f]; !ok {
			t.Errorf("SchemafErrorDetails has no %q", f)
		}
	}
}

// TestErrorDetailsRouteLevel: the framework route is registered at
// AuthOptional.
func TestErrorDetailsRouteLevel(t *testing.T) {
	for _, fr := range frameworkRoutes() {
		if fr.pattern == "GET /api/errors/{id}" {
			if fr.auth != AuthOptional {
				t.Errorf("auth = %v, want AuthOptional", fr.auth)
			}
			return
		}
	}
	t.Error("GET /api/errors/{id} is not a framework route")
}
