package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestBackendCRUD(t *testing.T) {
	base := testBackendURL(t)

	// Health check
	resp, err := http.Get(base + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: got %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Create
	body := `{"text":"buy milk"}`
	resp, err = http.Post(base+"/api/todos", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d, want 201", resp.StatusCode)
	}

	var created struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		Done bool   `json:"done"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	if created.Text != "buy milk" {
		t.Errorf("text: got %q, want %q", created.Text, "buy milk")
	}

	// List
	resp, err = http.Get(base + "/api/todos")
	if err != nil {
		t.Fatal(err)
	}
	var todos []map[string]any
	json.NewDecoder(resp.Body).Decode(&todos)
	resp.Body.Close()
	if len(todos) == 0 {
		t.Error("expected at least one todo")
	}

	// Get
	resp, err = http.Get(base + "/api/todos/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("get: got %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Update
	upd := `{"text":"buy oat milk","done":false}`
	req, _ := http.NewRequest(http.MethodPut, base+"/api/todos/"+created.ID, bytes.NewBufferString(upd))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("update: got %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Delete
	req, _ = http.NewRequest(http.MethodDelete, base+"/api/todos/"+created.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete: got %d, want 204", resp.StatusCode)
	}

	// Confirm gone
	resp, err = http.Get(base + "/api/todos/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get-after-delete: got %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}
