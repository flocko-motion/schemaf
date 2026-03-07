package tests

import (
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"schemaf.local/example/api"
)

// newAuthTSTestServer creates a test server with auth initialized using a fixed test key.
func newAuthTSTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	schemafapi.InitAuth([]byte("test-signing-key-32bytes-padding!"))
	schemafapi.Reset()
	api.Provider()
	srv := httptest.NewServer(schemafapi.NewMux())
	t.Cleanup(srv.Close)

	token, err := schemafapi.IssueToken("test-user-uuid", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}
	return srv, token
}

// runAuthTSTest runs a TS test with TEST_TOKEN set so the test can make authenticated requests.
func runAuthTSTest(t *testing.T, funcName string, srv *httptest.Server, token string) {
	t.Helper()
	cmd := exec.Command("npx", "tsx", "runner.gen.ts", funcName, srv.URL)
	cmd.Env = append(cmd.Environ(), "TEST_TOKEN="+token)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		t.Log(string(out))
	}
	if err != nil {
		t.Fatalf("TS test %q failed", funcName)
	}
}

func TestUserAuthFlow(t *testing.T) {
	srv, token := newAuthTSTestServer(t)
	runAuthTSTest(t, "testUserAuthFlow", srv, token)
}
