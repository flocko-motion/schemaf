package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	baseapi "schemaf.local/base/api"
	_ "schemaf.local/example/backend/api" // register routes via init()
)

func TestAPITS(t *testing.T) {
	// Routes are registered via init() in exampleapi; no DB needed for /openapi.ts.
	mux := http.NewServeMux()
	mux.Handle("GET /openapi.ts", baseapi.APITSHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/openapi.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("openapi.ts: got %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	ts := string(body)

	// Should contain async functions
	if !strings.Contains(ts, "async function") {
		t.Error("expected async function declarations")
	}

	// Should contain route-derived function names
	for _, fnName := range []string{"listTodos", "createTodo", "deleteTodo"} {
		if !strings.Contains(ts, fnName) {
			t.Errorf("missing function %q in openapi.ts", fnName)
		}
	}
}
