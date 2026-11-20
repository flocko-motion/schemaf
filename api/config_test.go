package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestPublicConfig(t *testing.T) {
	got := publicConfig([]string{
		"PUBLIC_SENTRY_DSN=https://x@sentry.example/1",
		"PUBLIC_EMPTY=",
		"PUBLIC_WITH_EQUALS=a=b",
		"DB_PASS=secret",
		"MY_PUBLIC_THING=nope",
		"public_lowercase=nope",
		"PUBLIC_NO_EQUALS_SIGN",
	})
	want := map[string]string{
		"PUBLIC_SENTRY_DSN":  "https://x@sentry.example/1",
		"PUBLIC_EMPTY":       "",
		"PUBLIC_WITH_EQUALS": "a=b",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestConfigEndpoint: GET /api/config on the full mux serves the PUBLIC_*
// variables without auth, with no-cache, and leaks nothing else.
func TestConfigEndpoint(t *testing.T) {
	t.Setenv("PUBLIC_TEST_DSN", "https://x@sentry.example/1")
	t.Setenv("PUBLIC_TEST_EMPTY", "")
	t.Setenv("TEST_PRIVATE_SECRET", "secret")

	srv := httptest.NewServer(testMux(t))
	defer srv.Close()

	// Changes after server start are not picked up: values are read once.
	t.Setenv("PUBLIC_TEST_LATE", "late")

	resp, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no auth required)", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if v, ok := body["PUBLIC_TEST_DSN"]; !ok || v != "https://x@sentry.example/1" {
		t.Errorf("PUBLIC_TEST_DSN = %q, %v", v, ok)
	}
	if v, ok := body["PUBLIC_TEST_EMPTY"]; !ok || v != "" {
		t.Errorf("PUBLIC_TEST_EMPTY = %q, %v; want present and empty", v, ok)
	}
	if _, ok := body["PUBLIC_TEST_LATE"]; ok {
		t.Error("PUBLIC_TEST_LATE set after start was served")
	}
	for k := range body {
		if !strings.HasPrefix(k, "PUBLIC_") {
			t.Errorf("non-public variable leaked: %s", k)
		}
	}
}

// TestFrameworkRoutesInSpec: /api/health, /api/status, /api/config and
// /api/user/me are in the framework's part of the spec, so the gate lets them
// through and the generated clients have them.
func TestFrameworkRoutesInSpec(t *testing.T) {
	paths, _ := FrameworkOpenAPI()
	for _, p := range []string{"/api/health", "/api/status", "/api/config", "/api/user/me"} {
		item, _ := paths[p].(map[string]any)
		if _, ok := item["get"]; !ok {
			t.Errorf("FrameworkOpenAPI has no GET %s", p)
		}
	}
}

// TestErrorSchemaListsFrameworkCodes: the Error schema's code description
// lists the codes schemaf writes itself, outside the sentinels.
func TestErrorSchemaListsFrameworkCodes(t *testing.T) {
	_, schemas := FrameworkOpenAPI()
	b, _ := json.Marshal(schemas[ErrorSchemaName])
	for _, code := range []string{"method_not_allowed", "not_acceptable", "unsupported_media_type", "not_found", "internal"} {
		if !strings.Contains(string(b), code) {
			t.Errorf("Error schema %s does not list %q", b, code)
		}
	}
}
