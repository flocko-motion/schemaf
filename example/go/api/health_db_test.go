package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	schemafdb "github.com/flocko-motion/schemaf/db"
)

func TestHealthWithDB(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL not set — run via ./schemaf.sh test")
	}
	if err := schemafdb.Init(dsn); err != nil {
		t.Fatalf("db.Init: %v", err)
	}

	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/health: got %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["db"] != "ok" {
		t.Fatalf("expected db:ok, got %q", body["db"])
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status:ok, got %q", body["status"])
	}
}
