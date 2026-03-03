package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// TestAPIRoundTrip performs a full CRUD cycle against a real running server.
// Set BACKEND_URL env var or run with a live server; skips if not available.
func TestAPIRoundTrip(t *testing.T) {
	base := testBackendURL(t)

	// Create
	payload := `{"text":"integration test todo"}`
	resp, err := http.Post(base+"/api/todos", "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Skipf("backend not reachable: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d, want 201", resp.StatusCode)
	}

	var created struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	json.NewDecoder(resp.Body).Decode(&created)

	// Get
	resp2, err := http.Get(base + "/api/todos/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("get: got %d, want 200", resp2.StatusCode)
	}

	// Delete
	req, _ := http.NewRequest(http.MethodDelete, base+"/api/todos/"+created.ID, nil)
	resp3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusNoContent {
		t.Errorf("delete: got %d, want 204", resp3.StatusCode)
	}
}
