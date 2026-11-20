// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flocko-motion/schemaf/log"
)

// errEndpoint is a typed endpoint that returns a fixed error.
type errEndpoint struct{ err error }

func (e errEndpoint) Method() string  { return "GET" }
func (e errEndpoint) Path() string    { return "/test/err" }
func (e errEndpoint) Auth() AuthLevel { return AuthNone }
func (e errEndpoint) Handle(context.Context, struct{}) (struct{}, error) {
	return struct{}{}, e.err
}

// errRawEndpoint is the raw twin of errEndpoint.
type errRawEndpoint struct{ err error }

func (e errRawEndpoint) Method() string                                     { return "GET" }
func (e errRawEndpoint) Path() string                                       { return "/test/raw/err" }
func (e errRawEndpoint) Auth() AuthLevel                                    { return AuthNone }
func (e errRawEndpoint) HandleRaw(http.ResponseWriter, *http.Request) error { return e.err }

// errBody is the decoded error response, with a map to see which keys exist.
type errBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
	ID    string `json:"id"`
	keys  map[string]any
}

func decodeErrBody(t *testing.T, resp *http.Response) errBody {
	t.Helper()
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	b := errBody{keys: raw}
	b.Error, _ = raw["error"].(string)
	b.Code, _ = raw["code"].(string)
	b.ID, _ = raw["id"].(string)
	return b
}

// serveErr answers one request to an endpoint returning err (typed, or raw
// when raw is set) and returns the response and its decoded body.
func serveErr(t *testing.T, err error, raw bool) (*http.Response, errBody) {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	path := "/test/err"
	if raw {
		Register(NewRawRoute(errRawEndpoint{err}, "", ""))
		path = "/test/raw/err"
	} else {
		Register(NewRoute[struct{}, struct{}](errEndpoint{err}, "", ""))
	}
	srv := httptest.NewServer(NewMux())
	t.Cleanup(srv.Close)
	resp, gerr := http.Get(srv.URL + path)
	if gerr != nil {
		t.Fatal(gerr)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	return resp, decodeErrBody(t, resp)
}

func TestErrorStatusAndDefaultCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
		msg    string
	}{
		{ErrBadRequest, 400, "bad_request", "bad request"},
		{ErrUnauthorized, 401, "unauthorized", "unauthorized"},
		{ErrForbidden, 403, "forbidden", "forbidden"},
		{ErrNotFound, 404, "not_found", "not found"},
		{ErrConflict, 409, "conflict", "conflict"},
		{ErrTooManyRequests, 429, "too_many_requests", "too many requests"},
		{errors.New("boom"), 500, "internal", "Internal Server Error"},
		{ErrUnavailable, 503, "unavailable", "Service Unavailable"},
		// Wrapped with %w: the sentinel anywhere in the chain counts.
		{fmt.Errorf("order 42: %w", ErrNotFound), 404, "not_found", "order 42: not found"},
	}
	for _, raw := range []bool{false, true} {
		for _, c := range cases {
			resp, body := serveErr(t, c.err, raw)
			if resp.StatusCode != c.status || body.Code != c.code || body.Error != c.msg {
				t.Errorf("raw=%v %v: got %d %+v, want %d code %q msg %q", raw, c.err, resp.StatusCode, body, c.status, c.code, c.msg)
			}
			if _, hasID := body.keys["id"]; hasID != (c.status >= 500) {
				t.Errorf("raw=%v %v: id present = %v", raw, c.err, hasID)
			}
		}
	}
}

// profaneName is an app error type with its own code and a sentinel.
type profaneName struct{}

func (profaneName) Error() string { return "this name is not allowed" }
func (profaneName) Code() string  { return "profane_name" }
func (profaneName) Unwrap() error { return ErrBadRequest }

// codedOnly carries a code but no sentinel.
type codedOnly struct{ code string }

func (e codedOnly) Error() string { return "maintenance in progress" }
func (e codedOnly) Code() string  { return e.code }

func TestErrorFunc(t *testing.T) {
	err := Error(ErrConflict, "last_head", "an organisation needs at least one head")
	if err.Error() != "an organisation needs at least one head" {
		t.Errorf("Error() = %q, want the message only", err.Error())
	}
	if !errors.Is(err, ErrConflict) {
		t.Error("errors.Is(err, ErrConflict) = false")
	}
	var c interface{ Code() string }
	if !errors.As(err, &c) || c.Code() != "last_head" {
		t.Errorf("Code() missing or wrong")
	}
}

func TestErrorCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		msg    string
	}{
		{"api.Error", Error(ErrConflict, "last_head", "an organisation needs at least one head"), 409, "last_head", "an organisation needs at least one head"},
		{"api.Error wrapped", fmt.Errorf("saving: %w", Error(ErrConflict, "last_head", "no head")), 409, "last_head", "saving: no head"},
		{"redundant default code", Error(ErrNotFound, "not_found", "order 42 is gone"), 404, "not_found", "order 42 is gone"},
		{"Code() hook without wrapping", profaneName{}, 400, "profane_name", "this name is not allowed"},
		{"Code() hook wrapped", fmt.Errorf("renaming: %w", profaneName{}), 400, "profane_name", "renaming: this name is not allowed"},
		{"coded without sentinel → 500 keeps code", codedOnly{"maintenance"}, 500, "maintenance", "Internal Server Error"},
		{"app code on 503", Error(ErrUnavailable, "maintenance", "down for maintenance"), 503, "maintenance", "Service Unavailable"},
		{"empty 4xx message → status text", Error(ErrConflict, "last_head", ""), 409, "last_head", "Conflict"},
		{"5xx message withheld", fmt.Errorf("pq: relation users does not exist: %w", ErrUnavailable), 503, "unavailable", "Service Unavailable"},
	}
	for _, c := range cases {
		resp, body := serveErr(t, c.err, false)
		if resp.StatusCode != c.status || body.Code != c.code || body.Error != c.msg {
			t.Errorf("%s: got %d %+v, want %d code %q msg %q", c.name, resp.StatusCode, body, c.status, c.code, c.msg)
		}
		if (c.status >= 500) != (len(body.ID) == 16) {
			t.Errorf("%s: id = %q", c.name, body.ID)
		}
	}
}

// captureLog routes the framework log into a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Logger()
	log.Set(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { log.Set(prev) })
	return &buf
}

// logEntries decodes the JSON log lines in buf.
func logEntries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// notSentinel is an error that is not one of schemaf's sentinels.
var notSentinel = errors.New("teapot")

func TestErrorMisuse(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		problem string // must appear in the log entry
	}{
		{"nil sentinel", Error(nil, "last_head", "x"), "nil sentinel"},
		{"foreign sentinel", Error(notSentinel, "last_head", "x"), "not a schemaf sentinel"},
		{"empty code from api.Error", Error(ErrConflict, "", "x"), "invalid code"},
		{"invalid code from api.Error", Error(ErrConflict, "Last-Head", "x"), "invalid code"},
		{"empty code from Code() hook", fmt.Errorf("x: %w", codedOnly{""}), "invalid code"},
		{"default code with wrong status", Error(ErrConflict, "not_found", "x"), "default code"},
		{"internal on a 400", Error(ErrBadRequest, "internal", "x"), "default code"},
		{"coded without sentinel using a 4xx default", codedOnly{"not_found"}, "default code"},
		{"Retry-After on 409", RetryAfter(ErrConflict, time.Minute), "Retry-After"},
		{"Retry-After on 500", RetryAfter(errors.New("boom"), time.Minute), "Retry-After"},
		{"Retry-After zero", RetryAfter(ErrTooManyRequests, 0), "Retry-After"},
		{"Retry-After negative", RetryAfter(ErrUnavailable, -time.Second), "Retry-After"},
		{"Retry-After nil error", RetryAfter(nil, time.Minute), "Retry-After"},
	}
	for _, c := range cases {
		buf := captureLog(t)
		resp, body := serveErr(t, c.err, false)
		if resp.StatusCode != 500 || body.Code != "internal" || body.Error != "Internal Server Error" || len(body.ID) != 16 {
			t.Errorf("%s: got %d %+v, want mute 500 internal with id", c.name, resp.StatusCode, body)
		}
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			t.Errorf("%s: Retry-After = %q on a misuse 500", c.name, ra)
		}
		entries := logEntries(t, buf)
		if len(entries) != 1 {
			t.Fatalf("%s: %d log entries, want 1: %s", c.name, len(entries), buf)
		}
		e := entries[0]
		if e["level"] != "ERROR" || e["id"] != body.ID {
			t.Errorf("%s: log entry = %v", c.name, e)
		}
		if m, _ := e["misuse"].(string); !strings.Contains(m, c.problem) {
			t.Errorf("%s: misuse = %q, want it to mention %q", c.name, m, c.problem)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	cases := []struct {
		err    error
		status int
		header string
	}{
		{RetryAfter(Error(ErrTooManyRequests, "token_locked", "too many failed attempts"), 90*time.Second), 429, "90"},
		{RetryAfter(ErrTooManyRequests, 1500*time.Millisecond), 429, "2"},
		{RetryAfter(ErrUnavailable, time.Nanosecond), 503, "1"},
		{fmt.Errorf("busy: %w", RetryAfter(ErrUnavailable, time.Hour)), 503, "3600"},
	}
	for _, c := range cases {
		resp, body := serveErr(t, c.err, false)
		if resp.StatusCode != c.status || resp.Header.Get("Retry-After") != c.header {
			t.Errorf("%v: got %d Retry-After %q, want %d %q", c.err, resp.StatusCode, resp.Header.Get("Retry-After"), c.status, c.header)
		}
		if c.status == 429 && body.Code == "internal" {
			t.Errorf("%v: answered as misuse", c.err)
		}
	}
	// The token_locked example keeps its code and message.
	_, body := serveErr(t, cases[0].err, false)
	if body.Code != "token_locked" || body.Error != "too many failed attempts" {
		t.Errorf("token_locked body = %+v", body)
	}
}

func TestServerErrorLog(t *testing.T) {
	buf := captureLog(t)
	_, body := serveErr(t, fmt.Errorf("pq: connection refused"), false)
	entries := logEntries(t, buf)
	if len(entries) != 1 {
		t.Fatalf("%d log entries, want 1", len(entries))
	}
	e := entries[0]
	want := map[string]any{
		"level": "ERROR", "id": body.ID, "method": "GET", "path": "/test/err",
		"status": float64(500), "code": "internal", "error": "pq: connection refused",
	}
	for k, v := range want {
		if e[k] != v {
			t.Errorf("log %s = %v, want %v", k, e[k], v)
		}
	}

	// 4xx are not logged.
	buf = captureLog(t)
	serveErr(t, ErrConflict, false)
	if buf.Len() != 0 {
		t.Errorf("4xx logged: %s", buf)
	}
}
