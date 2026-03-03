package tests

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestMigrations verifies that the backend started successfully with migrations applied
// by checking that the health endpoint reports the database as healthy.
func TestMigrations(t *testing.T) {
	base := testBackendURL(t)

	resp, err := http.Get(base + "/api/health")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: got %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body["db"] != "ok" {
		t.Errorf("db health: got %q, want %q", body["db"], "ok")
	}
}
