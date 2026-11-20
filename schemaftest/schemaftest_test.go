package schemaftest

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/cli"
	"github.com/flocko-motion/schemaf/constants"
	"github.com/flocko-motion/schemaf/db"
	"github.com/flocko-motion/schemaf/schemaf"
)

// The harness's own tests start containers: they run only with
// SCHEMAF_CONTAINER_TESTS=1, which e2e/db-test.sh sets.
func requireContainers(t *testing.T) {
	t.Helper()
	if os.Getenv("SCHEMAF_CONTAINER_TESTS") != "1" {
		t.Skip("SCHEMAF_CONTAINER_TESTS not set — skipping container test (run e2e/db-test.sh)")
	}
}

// What a project's go/app/constants.gen.go does.
func init() { constants.SetProjectName("harnesstest") }

//go:embed testdata/migrations/*.sql
var testMigrations embed.FS

// widgetsEndpoint answers the number of rows in widgets, so a test can see
// that the server reads what the test writes through DB().
type widgetsEndpoint struct{}

func (widgetsEndpoint) Method() string      { return "GET" }
func (widgetsEndpoint) Path() string        { return "/api/widgets/count" }
func (widgetsEndpoint) Auth() api.AuthLevel { return api.AuthNone }
func (widgetsEndpoint) HandleRaw(w http.ResponseWriter, r *http.Request) error {
	var n int
	if err := db.DB().QueryRowContext(r.Context(), `SELECT count(*) FROM widgets`).Scan(&n); err != nil {
		return err
	}
	fmt.Fprint(w, n)
	return nil
}

const testSpec = `{"openapi":"3.0.3","paths":{
"/api/health":{"get":{}},
"/api/status":{"get":{}},
"/api/config":{"get":{}},
"/api/user/me":{"get":{}},
"/api/auth/{provider}/login":{"post":{}},
"/api/auth/logout":{"post":{}},
"/api/auth/refresh":{"post":{}},
"/api/errors/{id}":{"get":{}},
"/api/widgets/count":{"get":{}}}}`

// wire is the test app's Wire.
func wire(app *schemaf.App) {
	app.AddDb(func() embed.FS { return testMigrations })
	app.AddApi(func() {
		api.SetOpenAPISpec(testSpec)
		api.Register(api.NewRawRoute(widgetsEndpoint{}, "", ""))
	})
}

func get(t *testing.T, url string, h http.Header) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h != nil {
		req.Header = h
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		b.WriteString(sc.Text())
	}
	return resp.StatusCode, b.String()
}

func TestStartServesHealthAndTokens(t *testing.T) {
	requireContainers(t)
	inst := Start(t, wire)

	if code, body := get(t, inst.URL()+"/api/health", nil); code != 200 || !strings.Contains(body, `"db":"ok"`) {
		t.Fatalf("GET /api/health = %d %s, want 200 with db ok", code, body)
	}
	tok := inst.Token("alice")
	if code, body := get(t, inst.URL()+"/api/user/me", inst.AuthHeader(tok)); code != 200 || !strings.Contains(body, "alice") {
		t.Fatalf("GET /api/user/me with a token = %d %s, want 200 for alice", code, body)
	}
	if code, _ := get(t, inst.URL()+"/api/user/me", inst.AuthHeader("not.a.token")); code != 401 {
		t.Fatalf("GET /api/user/me with an invalid token = %d, want 401", code)
	}
}

func TestDBSharesTheServersDatabase(t *testing.T) {
	requireContainers(t)
	inst := Start(t, wire)
	ctx := context.Background()

	var exists bool
	if err := inst.DB().QueryRowContext(ctx,
		`SELECT to_regclass('public.widgets') IS NOT NULL AND to_regclass('public._schemaf_sessions') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Fatalf("migrated schema not visible through DB(): exists=%v err=%v", exists, err)
	}
	if _, err := inst.DB().ExecContext(ctx, `INSERT INTO widgets (name) VALUES ('a'), ('b')`); err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, inst.URL()+"/api/widgets/count", nil); code != 200 || body != "2" {
		t.Fatalf("server sees %d %q, want 200 \"2\"", code, body)
	}
}

func TestRestartKeepsURLDataAndTokens(t *testing.T) {
	requireContainers(t)
	inst := Start(t, wire)
	url := inst.URL()
	tok := inst.Token("bob")
	if _, err := inst.DB().Exec(`INSERT INTO widgets (name) VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}

	inst.Restart()

	if inst.URL() != url {
		t.Fatalf("URL changed across Restart: %s → %s", url, inst.URL())
	}
	if code, body := get(t, url+"/api/widgets/count", nil); code != 200 || body != "1" {
		t.Fatalf("after Restart the server sees %d %q, want the row written before", code, body)
	}
	if code, _ := get(t, url+"/api/user/me", inst.AuthHeader(tok)); code != 200 {
		t.Fatalf("a token minted before Restart gets %d, want 200", code)
	}
}

// Sequential instances share nothing: their own signing key, their own
// database. Starting a second one also proves Wire running twice adds no
// duplicate routes (the route check would refuse to start) or migrations.
func TestSequentialInstancesAreIndependent(t *testing.T) {
	requireContainers(t)
	var tok1 string
	t.Run("first", func(t *testing.T) {
		inst := Start(t, wire)
		tok1 = inst.Token("carol")
		if _, err := inst.DB().Exec(`INSERT INTO widgets (name) VALUES ('first')`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("second", func(t *testing.T) {
		inst := Start(t, wire)
		if code, _ := get(t, inst.URL()+"/api/user/me", inst.AuthHeader(tok1)); code != 401 {
			t.Fatalf("the first instance's token gets %d on the second, want 401", code)
		}
		if code, body := get(t, inst.URL()+"/api/widgets/count", nil); body != "0" {
			t.Fatalf("the second instance sees %d %q, want no rows", code, body)
		}
		var n int
		if err := inst.DB().QueryRow(`SELECT count(*) FROM schemaf_migrations WHERE prefix = 'harnesstest'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("app migrations recorded %d times (err %v), want 1", n, err)
		}
	})
}

// fatalRecorder is a testing.TB whose Fatal records the message and ends
// the goroutine, so a test can check what Start refuses.
type fatalRecorder struct {
	testing.TB
	msg string
}

func (r *fatalRecorder) Fatal(args ...any) { r.msg = fmt.Sprint(args...); runtime.Goexit() }
func (r *fatalRecorder) Fatalf(f string, args ...any) {
	r.msg = fmt.Sprintf(f, args...)
	runtime.Goexit()
}
func (r *fatalRecorder) Helper() {}

// refusal runs Start on a recorder in its own goroutine and returns the
// Fatal message ("" if Start returned).
func refusal(t *testing.T) string {
	rec := &fatalRecorder{TB: t}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		Start(rec, wire)
	}()
	wg.Wait()
	return rec.msg
}

func TestSecondConcurrentStartFails(t *testing.T) {
	requireContainers(t)
	Start(t, wire)
	if msg := refusal(t); msg != errAnotherInstance {
		t.Fatalf("second Start: %q, want %q", msg, errAnotherInstance)
	}
}

// The guards need no container: they refuse before one starts.
func TestRefusesInsideDeployedContainer(t *testing.T) {
	t.Setenv("SCHEMAF_ENV", "docker")
	if msg := refusal(t); !strings.Contains(msg, "SCHEMAF_ENV=docker") {
		t.Fatalf("Start with SCHEMAF_ENV=docker: %q, want a refusal", msg)
	}
}

func TestPanicsOutsideTestBinary(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "go test") {
			t.Fatalf("recovered %v, want the test-binary panic", r)
		}
	}()
	requireTestBinary(false)
}

// The container dies with the test process, even on SIGKILL: a helper
// process starts an instance and is killed, then the container must go.
func TestContainerDiesWithSIGKILL(t *testing.T) {
	requireContainers(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldsContainer$", "-test.v")
	cmd.Env = append(os.Environ(), "SCHEMAFTEST_HELPER=1")
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	name := ""
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if n, ok := strings.CutPrefix(sc.Text(), "CONTAINER="); ok {
			name = n
			break
		}
	}
	if name == "" {
		cmd.Wait()
		t.Fatal("the helper never reported its container")
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()

	deadline := time.Now().Add(60 * time.Second)
	for {
		inspect, err := cli.Container("container", "inspect", name)
		if err != nil {
			t.Fatal(err)
		}
		if inspect.Run() != nil {
			return // gone
		}
		if time.Now().After(deadline) {
			t.Fatalf("container %s still exists 60s after its test process was killed", name)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestHelperHoldsContainer(t *testing.T) {
	if os.Getenv("SCHEMAFTEST_HELPER") != "1" {
		t.Skip("helper process for TestContainerDiesWithSIGKILL")
	}
	Start(t, wire)
	fmt.Printf("CONTAINER=%s\n", container.name)
	select {}
}

// An instance always runs in development mode, however the tests were
// started, and the environment is restored afterwards.
func TestInstanceRunsInDevelopmentMode(t *testing.T) {
	requireContainers(t)
	t.Setenv("SCHEMAF_DEV", "") // like a plain go test from the IDE
	t.Run("instance", func(t *testing.T) {
		inst := Start(t, wire)
		if code, body := get(t, inst.URL()+"/api/status", nil); code != 200 || !strings.Contains(body, `"mode":"development"`) {
			t.Fatalf("GET /api/status = %d %s, want development mode", code, body)
		}
	})
	if v := os.Getenv("SCHEMAF_DEV"); v != "" {
		t.Fatalf("SCHEMAF_DEV = %q after the instance, want it restored", v)
	}
}
