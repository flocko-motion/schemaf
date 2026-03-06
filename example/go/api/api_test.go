package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	schemafapi "schemaf.local/base/api"
	"schemaf.local/example/api"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	schemafapi.Reset()
	api.Provider()
	return httptest.NewServer(schemafapi.NewMux())
}

func newAuthTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	schemafapi.InitAuth([]byte("test-signing-key-32-bytes-padding"))
	return newTestServer(t)
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

func TestUserMe(t *testing.T) {
	srv := newAuthTestServer(t)
	defer srv.Close()

	token, err := schemafapi.IssueToken("user-uuid-123", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/user/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/user/me: got %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["id"] != "user-uuid-123" {
		t.Fatalf("expected id %q, got %q", "user-uuid-123", body["id"])
	}
}

func TestUserMeUnauthorized(t *testing.T) {
	srv := newAuthTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/user/me")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/user/me without token: got %d, want 401", resp.StatusCode)
	}
}

func TestUserMeInvalidToken(t *testing.T) {
	srv := newAuthTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/user/me", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", "not.a.valid.token"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/user/me with bad token: got %d, want 401", resp.StatusCode)
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
