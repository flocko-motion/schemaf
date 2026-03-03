package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	baseapi "atlas.local/base/api"
	_ "atlas.local/example/backend/api" // register routes via init()
)

func TestOpenAPI(t *testing.T) {
	// Routes are registered via init() in exampleapi; no DB needed for /openapi.json.
	mux := http.NewServeMux()
	mux.Handle("GET /openapi.json", baseapi.OpenAPIHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("openapi.json: got %d, want 200", resp.StatusCode)
	}

	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if doc.OpenAPI != "3.0.0" {
		t.Errorf("openapi version: got %q, want %q", doc.OpenAPI, "3.0.0")
	}

	// All todo paths should be present
	for _, path := range []string{"/api/todos", "/api/todos/{id}"} {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("missing path %q in openapi.json", path)
		}
	}
}
