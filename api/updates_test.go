package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withUpdatesDir points the update API at a temp dir holding files (name →
// content) and resets the authorizer afterwards.
func withUpdatesDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	old := updatesDir
	updatesDir = dir
	t.Cleanup(func() { updatesDir = old; updatesAuth = nil })
	return dir
}

const (
	testAvailable = `{"checked": "2026-11-22T10:00:00Z", "versions": [
  {"version": "1.5.0", "notes": "Faster search."},
  {"version": "1.6.0", "notes": ""}]}`
	testStatus = `{"running": "1.4.0", "previous": "1.3.0",
  "install": {"version": "1.4.0", "from": "1.3.0", "state": "installed", "started": "s", "finished": "f", "message": "installed 1.4.0", "backup": "/home/x/b.dump", "log": ""},
  "request": {"version": "1.2.0", "state": "rejected", "message": "rejected 1.2.0: not newer than the running 1.4.0", "time": "t"}}`
)

func enabledFiles() map[string]string {
	return map[string]string{"updater": "enabled\n", "request": "", "available.json": testAvailable, "status.json": testStatus}
}

func TestUpdatesWithoutUpdater(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", "myapp:1.4.0")
	withUpdatesDir(t, nil)
	st, err := Updates()
	if err != nil {
		t.Fatal(err)
	}
	if st.Running != "1.4.0" || st.Status != UpdatesNotEnabled || st.Available == nil || len(st.Available) != 0 {
		t.Errorf("Updates() = %+v, want the running version, an empty list, %q", st, UpdatesNotEnabled)
	}
	if err := RequestUpgrade("1.5.0"); !errors.Is(err, ErrUpdaterNotEnabled) {
		t.Errorf("RequestUpgrade without an updater = %v, want ErrUpdaterNotEnabled", err)
	}

	t.Run("in development there is no image", func(t *testing.T) {
		t.Setenv("SCHEMAF_IMAGE", "")
		st, _ := Updates()
		if st.Running != "" {
			t.Errorf("Running = %q", st.Running)
		}
	})
	t.Run("the marker without the request file is no updater", func(t *testing.T) {
		withUpdatesDir(t, map[string]string{"updater": "enabled\n", "available.json": testAvailable})
		if st, _ := Updates(); st.Status != UpdatesNotEnabled {
			t.Errorf("Status = %q", st.Status)
		}
	})
}

func TestUpdatesWithUpdater(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", "localhost/myapp:1.4.0")
	withUpdatesDir(t, enabledFiles())
	st, err := Updates()
	if err != nil {
		t.Fatal(err)
	}
	want := UpdateState{
		Running: "1.4.0", Status: UpdatesIdle, Checked: "2026-11-22T10:00:00Z",
		Available: []AvailableRelease{{"1.5.0", "Faster search."}, {"1.6.0", ""}},
		Install:   &InstallState{Version: "1.4.0", From: "1.3.0", State: "installed", Started: "s", Finished: "f", Message: "installed 1.4.0"},
		Request:   &RequestState{Version: "1.2.0", State: "rejected", Message: "rejected 1.2.0: not newer than the running 1.4.0", Time: "t"},
	}
	a, _ := json.Marshal(st)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Errorf("Updates() =\n%s\nwant\n%s", a, b)
	}
	if strings.Contains(string(a), "b.dump") {
		t.Error("Updates() exposes the run host's backup path")
	}

	t.Run("a broken file is an error, not an empty answer", func(t *testing.T) {
		files := enabledFiles()
		files["available.json"] = "{"
		withUpdatesDir(t, files)
		if _, err := Updates(); err == nil || !strings.Contains(err.Error(), "available.json is not valid JSON") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestRequestUpgrade(t *testing.T) {
	dir := withUpdatesDir(t, enabledFiles())
	for _, v := range []string{"1.4.0", "1.9.0", "", "1.5.0\nrm -rf /", "../1.5.0"} {
		if err := RequestUpgrade(v); !errors.Is(err, ErrVersionNotAvailable) {
			t.Errorf("RequestUpgrade(%q) = %v, want ErrVersionNotAvailable", v, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "request")); len(b) != 0 {
		t.Fatalf("a refused request wrote %q", b)
	}
	if err := RequestUpgrade("1.5.0"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "request")); string(b) != "1.5.0\n" {
		t.Errorf("request = %q, want the version", b)
	}

	t.Run("not while an install runs", func(t *testing.T) {
		files := enabledFiles()
		files["status.json"] = `{"running": "1.4.0", "install": {"version": "1.5.0", "from": "1.4.0", "state": "installing"}}`
		withUpdatesDir(t, files)
		if err := RequestUpgrade("1.6.0"); !errors.Is(err, ErrInstallRunning) {
			t.Errorf("err = %v, want ErrInstallRunning", err)
		}
		if st, _ := Updates(); st.Status != UpdatesInstalling {
			t.Errorf("Status = %q", st.Status)
		}
	})
}

// callUpdates runs an update endpoint's handler for a signed-in caller, as
// withAuth hands it on.
func callUpdates(t *testing.T, h http.Handler, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/api/updates", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), ctxKeyAuth{}, authInfo{subject: "alice"}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUpdateEndpoints(t *testing.T) {
	dir := withUpdatesDir(t, enabledFiles())

	t.Run("closed until the app authorizes someone", func(t *testing.T) {
		SetUpdatesAuth(nil)
		for _, c := range []struct {
			h      http.Handler
			method string
		}{{updatesHandler(), "GET"}, {updatesRequestHandler(), "POST"}} {
			w := callUpdates(t, c.h, c.method, `{"version": "1.5.0"}`)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "updates: the app has not authorized anyone (app.SetUpdatesAuth)") {
				t.Errorf("%s: %d %s", c.method, w.Code, w.Body)
			}
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "request")); len(b) != 0 {
			t.Errorf("an unauthorized request wrote %q", b)
		}
	})

	t.Run("the authorizer's error is the answer", func(t *testing.T) {
		SetUpdatesAuth(func(ctx context.Context) error {
			return Error(ErrForbidden, "forbidden", "only admins manage updates")
		})
		w := callUpdates(t, updatesHandler(), "GET", "")
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "only admins manage updates") {
			t.Errorf("%d %s", w.Code, w.Body)
		}
	})

	SetUpdatesAuth(func(ctx context.Context) error {
		if sub, _ := Subject(ctx); sub != "alice" {
			return Error(ErrForbidden, "forbidden", "not alice")
		}
		return nil
	})

	t.Run("GET /api/updates", func(t *testing.T) {
		w := callUpdates(t, updatesHandler(), "GET", "")
		var st UpdateState
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &st) != nil || len(st.Available) != 2 {
			t.Errorf("%d %s", w.Code, w.Body)
		}
	})

	t.Run("POST /api/updates/request", func(t *testing.T) {
		w := callUpdates(t, updatesRequestHandler(), "POST", `{"version": "1.6.0"}`)
		if w.Code != http.StatusAccepted || strings.TrimSpace(w.Body.String()) != `{"version":"1.6.0"}` {
			t.Errorf("%d %s", w.Code, w.Body)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "request")); string(b) != "1.6.0\n" {
			t.Errorf("request = %q", b)
		}
		for body, want := range map[string]int{
			`{"version": "1.9.0"}`:         http.StatusUnprocessableEntity,
			`{"version": ""}`:              http.StatusBadRequest,
			`{"version": "1.5.0", "x": 1}`: http.StatusBadRequest,
			`not json`:                     http.StatusBadRequest,
			`{"version": "` + strings.Repeat("1", 2000) + `"}`: http.StatusBadRequest,
		} {
			if w := callUpdates(t, updatesRequestHandler(), "POST", body); w.Code != want {
				t.Errorf("body %.40s: %d, want %d: %s", body, w.Code, want, w.Body)
			}
		}
	})

	t.Run("without an updater: 409 updater_not_enabled", func(t *testing.T) {
		withUpdatesDir(t, nil)
		SetUpdatesAuth(func(ctx context.Context) error { return nil })
		w := callUpdates(t, updatesRequestHandler(), "POST", `{"version": "1.5.0"}`)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "updater_not_enabled") {
			t.Errorf("%d %s", w.Code, w.Body)
		}
		w = callUpdates(t, updatesHandler(), "GET", "")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"updater not enabled"`) {
			t.Errorf("%d %s", w.Code, w.Body)
		}
	})
}

func TestUpdateEndpointsInSpec(t *testing.T) {
	paths, schemas := FrameworkOpenAPI()
	for path, method := range map[string]string{"/api/updates": "get", "/api/updates/request": "post", "/api/updates/check": "post"} {
		item, _ := paths[path].(map[string]any)
		op, _ := item[method].(map[string]any)
		if op == nil || op["security"] == nil {
			t.Errorf("FrameworkOpenAPI has no %s %s with security", method, path)
		}
	}
	for _, name := range []string{"SchemafUpdateState", "SchemafUpdateRequest"} {
		if schemas[name] == nil {
			t.Errorf("no schema %s", name)
		}
	}
}

func TestAvailableOnlyNewerThanRunning(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", "myapp:1.5.0")
	withUpdatesDir(t, enabledFiles()) // lists 1.5.0 and 1.6.0
	st, err := Updates()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Available) != 1 || st.Available[0].Version != "1.6.0" {
		t.Errorf("Available = %+v, want only 1.6.0 (1.5.0 runs)", st.Available)
	}
	if err := RequestUpgrade("1.5.0"); !errors.Is(err, ErrVersionNotAvailable) {
		t.Errorf("requesting the running version: %v", err)
	}
}

func TestRunningVersionOnlyForVersionTags(t *testing.T) {
	for img, want := range map[string]string{
		"myapp:1.4.0": "1.4.0", "localhost/myapp:2.0.0-rc.1": "2.0.0-rc.1",
		"myapp:3f9a2c1": "", "registry:5000/myapp": "", "": "", "myapp:3f9a2c1-dirty": "",
	} {
		t.Setenv("SCHEMAF_IMAGE", img)
		if got := runningVersion(); got != want {
			t.Errorf("SCHEMAF_IMAGE=%q: %q, want %q", img, got, want)
		}
	}
}

func TestNewerVersion(t *testing.T) {
	// Each is newer than the one before (semver.org's precedence example).
	order := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.2.0", "1.10.0", "2.0.0"}
	for i := range order {
		for j := range order {
			if got := newerVersion(order[i], order[j]); got != (i > j) {
				t.Errorf("newerVersion(%s, %s) = %v", order[i], order[j], got)
			}
		}
	}
}

// checkFiles is an enabled updater whose status.json holds check, a JSON
// object or "" for none, and whose available.json was refreshed at checked.
func checkFiles(check, checked string) map[string]string {
	files := enabledFiles()
	files["available.json"] = `{"checked": "` + checked + `", "versions": [{"version": "1.5.0", "notes": ""}]}`
	if check != "" {
		files["status.json"] = `{"running": "1.4.0", "check": ` + check + `}`
	} else {
		files["status.json"] = `{"running": "1.4.0"}`
	}
	return files
}

func withNow(t *testing.T, at time.Time) {
	t.Helper()
	old := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = old })
}

func TestCheckForUpdates(t *testing.T) {
	at := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) string { return at.Add(-d).Format(time.RFC3339) }
	withNow(t, at)

	t.Run("writes check to the request file", func(t *testing.T) {
		dir := withUpdatesDir(t, checkFiles(`{"state": "checked", "trigger": "timer", "started": "`+ago(3*time.Hour)+`", "finished": "`+ago(2*time.Hour)+`", "message": "checked"}`, ago(2*time.Hour)))
		if err := CheckForUpdates(); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "request")); string(b) != "check\n" {
			t.Errorf("request = %q, want check", b)
		}
	})

	for _, c := range []struct {
		name  string
		files map[string]string
		want  error
		msg   string
	}{
		{"without an updater", nil, ErrUpdaterNotEnabled, ""},
		{"while an install runs", func() map[string]string {
			f := checkFiles("", ago(time.Hour))
			f["status.json"] = `{"running": "1.4.0", "install": {"version": "1.5.0", "from": "1.4.0", "state": "installing"}}`
			return f
		}(), ErrInstallRunning, ""},
		{"while a check runs", checkFiles(`{"state": "checking", "trigger": "timer", "started": "`+ago(10*time.Second)+`", "finished": "", "message": ""}`, ago(time.Hour)), ErrCheckRunning, ""},
		{"right after a check finished", checkFiles(`{"state": "checked", "trigger": "app", "started": "`+ago(30*time.Second)+`", "finished": "`+ago(20*time.Second)+`", "message": ""}`, ago(20*time.Second)), ErrCheckTooSoon, "last check finished 20s ago — check again in 40s"},
		{"right after a failed check", checkFiles(`{"state": "failed", "trigger": "app", "started": "`+ago(30*time.Second)+`", "finished": "`+ago(15*time.Second)+`", "message": "could not download"}`, ago(time.Hour)), ErrCheckTooSoon, "check again in 45s"},
		{"a rejected check doesn't hide the last refresh", checkFiles(`{"state": "rejected", "trigger": "app", "started": "`+ago(time.Second)+`", "finished": "`+ago(time.Second)+`", "message": "too soon"}`, ago(30*time.Second)), ErrCheckTooSoon, "check again in 30s"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := withUpdatesDir(t, c.files)
			err := CheckForUpdates()
			if !errors.Is(err, c.want) || !strings.Contains(fmt.Sprint(err), c.msg) {
				t.Fatalf("err = %v, want %v containing %q", err, c.want, c.msg)
			}
			if b, _ := os.ReadFile(filepath.Join(dir, "request")); len(b) != 0 {
				t.Errorf("a refused check wrote %q", b)
			}
		})
	}

	t.Run("an interrupted check (no finished time) doesn't count for the limit", func(t *testing.T) {
		withUpdatesDir(t, checkFiles(`{"state": "failed", "trigger": "timer", "started": "`+ago(time.Hour)+`", "finished": "", "message": "the check was interrupted"}`, ago(2*time.Hour)))
		if err := CheckForUpdates(); err != nil {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("a check older than 30 minutes counts as interrupted", func(t *testing.T) {
		dir := withUpdatesDir(t, checkFiles(`{"state": "checking", "trigger": "timer", "started": "`+ago(31*time.Minute)+`", "finished": "", "message": ""}`, ago(time.Hour)))
		st, err := Updates()
		if err != nil || st.Status != UpdatesIdle {
			t.Errorf("Status = %q, %v; want idle", st.Status, err)
		}
		if err := CheckForUpdates(); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "request")); string(b) != "check\n" {
			t.Errorf("request = %q", b)
		}
	})

	t.Run("Updates reports the check", func(t *testing.T) {
		withUpdatesDir(t, checkFiles(`{"state": "checking", "trigger": "app", "started": "`+ago(5*time.Second)+`", "finished": "", "message": "checking for new releases"}`, ago(time.Hour)))
		st, err := Updates()
		if err != nil {
			t.Fatal(err)
		}
		want := &CheckState{State: "checking", Trigger: "app", Started: ago(5 * time.Second), Message: "checking for new releases"}
		if st.Status != UpdatesChecking || st.Check == nil || *st.Check != *want {
			t.Errorf("Status %q, Check %+v; want checking, %+v", st.Status, st.Check, want)
		}
	})

	t.Run("an install outranks a check", func(t *testing.T) {
		f := checkFiles("", ago(time.Hour))
		f["status.json"] = `{"running": "1.4.0", "install": {"version": "1.5.0", "from": "1.4.0", "state": "installing"},
		  "check": {"state": "checking", "trigger": "timer", "started": "` + ago(time.Second) + `", "finished": "", "message": ""}}`
		withUpdatesDir(t, f)
		if st, _ := Updates(); st.Status != UpdatesInstalling {
			t.Errorf("Status = %q", st.Status)
		}
	})

	t.Run("an empty check record from an older run side is no check", func(t *testing.T) {
		withUpdatesDir(t, checkFiles(`{"state": "", "trigger": "", "started": "", "finished": "", "message": ""}`, ago(time.Hour)))
		if st, _ := Updates(); st.Check != nil {
			t.Errorf("Check = %+v", st.Check)
		}
	})
}

func TestCheckEndpoint(t *testing.T) {
	withNow(t, time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	dir := withUpdatesDir(t, checkFiles("", "2026-11-23T10:00:00Z"))

	SetUpdatesAuth(nil)
	if w := callUpdates(t, updatesCheckHandler(), "POST", ""); w.Code != http.StatusForbidden {
		t.Errorf("unauthorized: %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "request")); len(b) != 0 {
		t.Errorf("an unauthorized check wrote %q", b)
	}

	SetUpdatesAuth(func(ctx context.Context) error { return nil })
	w := callUpdates(t, updatesCheckHandler(), "POST", "")
	if w.Code != http.StatusAccepted || w.Body.Len() != 0 {
		t.Errorf("%d %q, want 202 without content", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "request")); string(b) != "check\n" {
		t.Errorf("request = %q", b)
	}

	t.Run("too soon: 429 with Retry-After", func(t *testing.T) {
		withUpdatesDir(t, checkFiles(`{"state": "checked", "trigger": "app", "started": "2026-11-23T11:59:30Z", "finished": "2026-11-23T11:59:40Z", "message": ""}`, "2026-11-23T11:59:40Z"))
		SetUpdatesAuth(func(ctx context.Context) error { return nil })
		w := callUpdates(t, updatesCheckHandler(), "POST", "")
		if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "check_too_soon") || w.Header().Get("Retry-After") != "40" {
			t.Errorf("%d %s Retry-After=%q", w.Code, w.Body, w.Header().Get("Retry-After"))
		}
	})

	t.Run("running: 409 check_running", func(t *testing.T) {
		withUpdatesDir(t, checkFiles(`{"state": "checking", "trigger": "timer", "started": "2026-11-23T11:59:50Z", "finished": "", "message": ""}`, "2026-11-23T10:00:00Z"))
		SetUpdatesAuth(func(ctx context.Context) error { return nil })
		w := callUpdates(t, updatesCheckHandler(), "POST", "")
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "check_running") {
			t.Errorf("%d %s", w.Code, w.Body)
		}
	})
}
