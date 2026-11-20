// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeErrorDirect answers err through a bare http.HandlerFunc calling
// WriteError, outside schemaf's mux, as a standalone server would.
func writeErrorDirect(t *testing.T, err error) *http.Response {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, err)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/standalone/err", nil))
	return rec.Result()
}

// comparableBody decodes an error body and replaces a present id by a
// marker, so two responses compare equal when only their ids differ.
func comparableBody(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if id, ok := m["id"].(string); ok && len(id) == 16 {
		m["id"] = "<present>"
	}
	return m
}

// TestWriteErrorMatchesEndpoint: a bare handler calling WriteError answers
// exactly as a raw endpoint returning the same error: status, headers, body
// (the id only compared for presence).
func TestWriteErrorMatchesEndpoint(t *testing.T) {
	captureLog(t)
	errs := []error{
		Error(ErrForbidden, "denied", "it is not your turn"),
		RetryAfter(Error(ErrTooManyRequests, "token_locked", "too many failed attempts"), 90*time.Second),
		RetryAfter(ErrUnavailable, time.Minute),
		Error(ErrConflict, "not_found", "misuse: default code with the wrong status"),
		errors.New("pq: connection refused"),
		fmt.Errorf("order 42: %w", ErrNotFound),
		Error(ErrUnprocessable, "bad_move", "that move is not allowed"),
	}
	for _, s := range sentinels {
		errs = append(errs, s.err, fmt.Errorf("wrapped: %w", s.err))
	}
	for _, err := range errs {
		want, _ := serveErrRaw(t, err)
		got := writeErrorDirect(t, err)
		if got.StatusCode != want.StatusCode {
			t.Errorf("%v: status %d, endpoint %d", err, got.StatusCode, want.StatusCode)
		}
		for _, h := range []string{"Content-Type", "Retry-After"} {
			if got.Header.Get(h) != want.Header.Get(h) {
				t.Errorf("%v: %s %q, endpoint %q", err, h, got.Header.Get(h), want.Header.Get(h))
			}
		}
		gb, wb := comparableBody(t, got.Body), comparableBody(t, want.Body)
		if !reflect.DeepEqual(gb, wb) {
			t.Errorf("%v: body %v, endpoint %v", err, gb, wb)
		}
	}
}

// serveErrRaw answers one request to a raw endpoint returning err and
// returns the response with its body unread.
func serveErrRaw(t *testing.T, err error) (*http.Response, string) {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	Register(NewRawRoute(errRawEndpoint{err}, "", ""))
	srv := httptest.NewServer(testMux(t))
	t.Cleanup(srv.Close)
	resp, gerr := http.Get(srv.URL + "/api/test/raw/err")
	if gerr != nil {
		t.Fatal(gerr)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp, srv.URL
}

// TestNewSentinels: each added sentinel answers its status and default
// code, and its default code is misuse with another sentinel.
func TestNewSentinels(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrGone, 410, "gone"},
		{ErrPreconditionFailed, 412, "precondition_failed"},
		{ErrTooLarge, 413, "too_large"},
		{ErrUnprocessable, 422, "unprocessable"},
		{ErrNotImplemented, 501, "not_implemented"},
		{ErrBadGateway, 502, "bad_gateway"},
		{ErrGatewayTimeout, 504, "gateway_timeout"},
	}
	captureLog(t)
	for _, c := range cases {
		resp, body := serveErr(t, c.err, false)
		if resp.StatusCode != c.status || body.Code != c.code {
			t.Errorf("%v: got %d %q, want %d %q", c.err, resp.StatusCode, body.Code, c.status, c.code)
		}
		wantMsg := c.err.Error()
		if c.status >= 500 {
			wantMsg = http.StatusText(c.status)
		}
		if body.Error != wantMsg {
			t.Errorf("%v: message %q, want %q", c.err, body.Error, wantMsg)
		}
		if (body.ID != "") != (c.status >= 500) {
			t.Errorf("%v: id %q", c.err, body.ID)
		}
		// Valid in api.Error with an own code.
		if resp, body := serveErr(t, Error(c.err, "own_code", "x"), false); resp.StatusCode != c.status || body.Code != "own_code" {
			t.Errorf("api.Error(%v, own_code): got %d %q", c.err, resp.StatusCode, body.Code)
		}
		// Its default code with another sentinel is misuse.
		if resp, body := serveErr(t, Error(ErrConflict, c.code, "x"), false); resp.StatusCode != 500 || body.Code != "internal" {
			t.Errorf("api.Error(ErrConflict, %q): got %d %q, want misuse", c.code, resp.StatusCode, body.Code)
		}
		// Retry-After stays limited to 429 and 503.
		if resp, _ := serveErr(t, RetryAfter(c.err, time.Minute), false); resp.StatusCode != 500 {
			t.Errorf("RetryAfter(%v): got %d, want misuse", c.err, resp.StatusCode)
		}
	}
}

// TestWriteErrorNil: a nil error is misuse: a mute 500 with an id, and the
// log names the problem.
func TestWriteErrorNil(t *testing.T) {
	buf := captureLog(t)
	resp := writeErrorDirect(t, nil)
	var body errorResponse
	json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != 500 || body.Code != "internal" || body.Error != "Internal Server Error" || len(body.ID) != 16 {
		t.Fatalf("got %d %+v, want a mute 500 internal with an id", resp.StatusCode, body)
	}
	entries := logEntries(t, buf)
	if len(entries) != 1 {
		t.Fatalf("%d log entries, want 1: %s", len(entries), buf)
	}
	if m, _ := entries[0]["misuse"].(string); !strings.Contains(m, "api.WriteError called with a nil error") {
		t.Errorf("misuse = %q", m)
	}
	if entries[0]["id"] != body.ID {
		t.Errorf("log id = %v, want %s", entries[0]["id"], body.ID)
	}
}

// TestErrorSchemaListsNewCodes: the OpenAPI Error.code description lists
// the default codes of the added sentinels.
func TestErrorSchemaListsNewCodes(t *testing.T) {
	_, schemas := FrameworkOpenAPI()
	b, _ := json.Marshal(schemas[ErrorSchemaName])
	for _, code := range []string{"gone", "precondition_failed", "too_large", "unprocessable", "not_implemented", "bad_gateway", "gateway_timeout"} {
		if !strings.Contains(string(b), code) {
			t.Errorf("Error schema does not list %q", code)
		}
	}
}
