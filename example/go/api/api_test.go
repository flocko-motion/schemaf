package api_test

import (
	"context"
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

func TestMalformedJSONReturns400(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/todos", "application/json",
		strings.NewReader(`{not valid json`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed JSON: got %d, want 400", resp.StatusCode)
	}
}

func TestNotFoundReturns404(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// GetTodoEndpoint returns ErrNotFound when ID is empty — hit /api/todos/ with no id segment.
	// To force the handler to return ErrNotFound, call a non-existent route.
	resp, err := http.Get(srv.URL + "/api/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route: got %d, want 404", resp.StatusCode)
	}
}

func TestHandlerErrNotFoundMapsTo404(t *testing.T) {
	// Register a one-off endpoint that always returns ErrNotFound.
	schemafapi.Reset()
	schemafapi.Register(schemafapi.NewRoute[struct{}, struct{}](notFoundEndpoint{}, "", ""))
	srv := httptest.NewServer(schemafapi.NewMux())
	defer srv.Close()

	r, err := http.Get(srv.URL + "/api/test/notfound")
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("ErrNotFound handler: got %d, want 404", r.StatusCode)
	}
}

type notFoundEndpoint struct{}

func (notFoundEndpoint) Method() string { return "GET" }
func (notFoundEndpoint) Path() string   { return "/api/test/notfound" }
func (notFoundEndpoint) Auth() bool     { return false }
func (notFoundEndpoint) Handle(_ context.Context, _ struct{}) (struct{}, error) {
	return struct{}{}, schemafapi.ErrNotFound
}

func TestGetTodoPathParam(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	const wantID = "abc-123-uuid"
	resp, err := http.Get(srv.URL + "/api/todos/" + wantID)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/todos/{id}: got %d, want 200", resp.StatusCode)
	}
	var body struct {
		Todo struct {
			ID string `json:"id"`
		} `json:"todo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Todo.ID != wantID {
		t.Fatalf("path param not decoded: got id=%q, want %q", body.Todo.ID, wantID)
	}
}

func TestStatus(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status: got %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", body["status"])
	}
	if _, ok := body["uptime"]; !ok {
		t.Fatal("expected uptime field in /api/status response")
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
