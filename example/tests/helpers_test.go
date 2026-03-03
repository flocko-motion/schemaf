package tests

import (
	"os"
	"testing"
)

// testBackendURL returns BACKEND_URL from the environment or skips the test.
// test.sh sets this when it starts the native backend.
func testBackendURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("BACKEND_URL")
	if u == "" {
		t.Skip("BACKEND_URL not set; skipping integration test")
	}
	return u
}

// testNginxURL returns NGINX_URL from the environment or skips the test.
func testNginxURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("NGINX_URL")
	if u == "" {
		t.Skip("NGINX_URL not set; skipping nginx test")
	}
	return u
}
