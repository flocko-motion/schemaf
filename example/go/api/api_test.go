package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	schemafapi "schemaf.local/base/api"
	"schemaf.local/example/api"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	schemafapi.Reset()
	api.Provider()
	return httptest.NewServer(schemafapi.NewMux())
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: got %d, want 200", resp.StatusCode)
	}
}

func TestOpenAPI(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("openapi.json: got %d, want 200", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("openapi.json: invalid JSON: %v", err)
	}
	if doc["openapi"] != "3.0.0" {
		t.Fatalf("openapi.json: expected openapi 3.0.0, got %v", doc["openapi"])
	}
}

func TestListTodos(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/todos")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/todos: got %d, want 200", resp.StatusCode)
	}
	var body api.ListTodosResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("GET /api/todos: decode: %v", err)
	}
	if body.Todos == nil {
		t.Fatal("GET /api/todos: expected non-nil todos array")
	}
}

func TestCreateTodo(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/todos", "application/json",
		strings.NewReader(`{"text":"buy milk"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/todos: got %d, want 200", resp.StatusCode)
	}
	var todo api.Todo
	if err := json.NewDecoder(resp.Body).Decode(&todo); err != nil {
		t.Fatalf("POST /api/todos: decode: %v", err)
	}
	if todo.Text != "buy milk" {
		t.Fatalf("POST /api/todos: expected text 'buy milk', got %q", todo.Text)
	}
}

func TestProviderRegistersAllEndpoints(t *testing.T) {
	schemafapi.Reset()
	api.Provider()
	routes := schemafapi.Routes()
	if len(routes) != 6 {
		t.Fatalf("expected 6 endpoints, got %d", len(routes))
	}
}
