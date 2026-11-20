package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAPIServesSetSpec: /openapi.json is the spec the generated
// Provider() set, byte for byte; no spec is built at runtime.
func TestOpenAPIServesSetSpec(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const spec = "{\n  \"openapi\": \"3.0.0\",\n  \"x-marker\": \"from codegen\"\n}"
	SetOpenAPISpec(spec)
	rec := httptest.NewRecorder()
	NewMux().ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := rec.Body.String(); got != spec {
		t.Errorf("body = %q, want %q", got, spec)
	}
}

// TestOpenAPIWithoutSpec: without a spec (Provider not wired) the route
// answers a 500 error, never an empty or made-up spec.
func TestOpenAPIWithoutSpec(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	captureLog(t)
	rec := httptest.NewRecorder()
	NewMux().ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != "internal" || body.ID == "" {
		t.Errorf("body = %s (%v), want an internal error with an id", rec.Body, err)
	}
}
