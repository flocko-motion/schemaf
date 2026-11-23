// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// failingRawEndpoint answers every request with a plain error: a 500.
type failingRawEndpoint struct{}

func (failingRawEndpoint) Method() string  { return "GET" }
func (failingRawEndpoint) Path() string    { return "/api/test/fails" }
func (failingRawEndpoint) Auth() AuthLevel { return AuthNone }
func (failingRawEndpoint) HandleRaw(http.ResponseWriter, *http.Request) error {
	return errors.New("boom")
}

// frameOptionsRawEndpoint sets X-Frame-Options itself.
type frameOptionsRawEndpoint struct{}

func (frameOptionsRawEndpoint) Method() string  { return "GET" }
func (frameOptionsRawEndpoint) Path() string    { return "/api/test/frame" }
func (frameOptionsRawEndpoint) Auth() AuthLevel { return AuthNone }
func (frameOptionsRawEndpoint) HandleRaw(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// testIndexHTML is the embedded index.html of the header tests.
const testIndexHTML = "<!doctype html><html><head><title>t</title></head><body>frontend</body></html>"

// responseKind is one kind of response the server writes.
type responseKind struct {
	name, method, path string
	status             int
}

// responseKinds lists a request for every kind of response: an endpoint,
// a framework route, /openapi.json, a frontend file, the SPA fallback, the
// gate's 404 and 405, and an error.
var responseKinds = []responseKind{
	{"endpoint", "GET", "/api/test/typed", 200},
	{"framework route", "GET", "/api/health", 200},
	{"openapi.json", "GET", "/openapi.json", 200},
	{"frontend file", "GET", "/app.js", 200},
	{"spa fallback", "GET", "/some/client/route", 200},
	{"gate 404", "GET", "/api/does-not-exist", 404},
	{"gate 405", "POST", "/api/health", 405},
	{"error", "GET", "/api/test/fails", 500},
}

// headerServer starts a test server with the header tests' routes and
// frontend, through NewHandler as the server does. proxy selects the dev
// proxy to a stand-in Vite server instead of the embedded frontend.
func headerServer(t *testing.T, proxy bool) string {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	Register(NewRoute(pathEndpoint{"GET", "/api/test/typed"}, "", ""))
	Register(NewRawRoute(failingRawEndpoint{}, "", ""))
	Register(NewRawRoute(frameOptionsRawEndpoint{}, "", ""))
	SetOpenAPISpec(specFor(Routes()))
	SetOnResponseHeaders(pendingHook)
	SetFrontend(fstest.MapFS{
		"index.html": {Data: []byte(testIndexHTML)},
		"app.js":     {Data: []byte("console.log(1)")},
	})
	h, err := NewHandler(proxy)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// get answers one request with an empty body and returns the response and its body.
func get(t *testing.T, method, url string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// Every kind of response carries X-Schemaf-Version with the image's tag.
func TestVersionHeaderOnEveryResponse(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", "myapp:1.5.0")
	url := headerServer(t, false)
	for _, k := range responseKinds {
		t.Run(k.name, func(t *testing.T) {
			resp, body := get(t, k.method, url+k.path)
			if resp.StatusCode != k.status {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, k.status, body)
			}
			if got := resp.Header.Values("X-Schemaf-Version"); len(got) != 1 || got[0] != "1.5.0" {
				t.Errorf("X-Schemaf-Version = %q, want [1.5.0]", got)
			}
		})
	}
}

// The dev proxy's responses carry the header too.
func TestVersionHeaderOnDevProxy(t *testing.T) {
	frontendModes(t, func(t *testing.T) {
		if !DevMode() {
			return
		}
		url := headerServer(t, true)
		resp, body := get(t, "GET", url+"/")
		if body != "frontend" {
			t.Fatalf("body %q, want the dev server's", body)
		}
		if got := resp.Header.Get("X-Schemaf-Version"); got != "dev" {
			t.Errorf("X-Schemaf-Version = %q, want dev", got)
		}
	})
}

// The version is the image's tag as it is, or dev without an image.
func TestRunningVersion(t *testing.T) {
	for _, tc := range []struct{ image, want string }{
		{"myapp:1.5.0", "1.5.0"},
		{"myapp:2.0.0-dev.3", "2.0.0-dev.3"},
		{"myapp:3f9a2c1", "3f9a2c1"},
		{"myapp:3f9a2c1-dirty", "3f9a2c1-dirty"},
		{"myapp:staging_7", "staging_7"},
		{"", "dev"},
		{"myapp:dev-runs-natively", "dev"},
	} {
		t.Run(tc.image, func(t *testing.T) {
			t.Setenv("SCHEMAF_IMAGE", tc.image)
			if got := RunningVersion(); got != tc.want {
				t.Errorf("RunningVersion() = %q, want %q", got, tc.want)
			}
			url := headerServer(t, false)
			resp, _ := get(t, "GET", url+"/api/health")
			if got := resp.Header.Get("X-Schemaf-Version"); got != tc.want {
				t.Errorf("X-Schemaf-Version = %q, want %q", got, tc.want)
			}
		})
	}
}

// api.Updates().Running stays semver-or-empty.
func TestUpdatesRunningStaysARelease(t *testing.T) {
	old := updatesDir
	updatesDir = t.TempDir()
	t.Cleanup(func() { updatesDir = old })
	t.Setenv("SCHEMAF_IMAGE", "myapp:3f9a2c1")
	st, err := Updates()
	if err != nil {
		t.Fatal(err)
	}
	if st.Running != "" {
		t.Errorf("Updates().Running = %q for a commit tag, want empty", st.Running)
	}
}

// withHook sets the hook for the next headerServer.
func withHook(t *testing.T, hook ResponseHeadersFunc) {
	t.Helper()
	pendingHook = hook
	t.Cleanup(func() { pendingHook = nil })
}

// pendingHook is the hook headerServer sets after its Reset.
var pendingHook ResponseHeadersFunc

// The hook runs on every kind of response and sees schemaf's headers.
func TestResponseHeadersHookOnEveryResponse(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", "myapp:1.5.0")
	var sawVersion []string
	withHook(t, func(r *http.Request, h http.Header) {
		sawVersion = append(sawVersion, h.Get("X-Schemaf-Version"))
		h.Set("X-Content-Type-Options", "nosniff")
	})
	url := headerServer(t, false)
	for _, k := range responseKinds {
		t.Run(k.name, func(t *testing.T) {
			resp, body := get(t, k.method, url+k.path)
			if resp.StatusCode != k.status {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, k.status, body)
			}
			if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
	}
	for _, v := range sawVersion {
		if v != "1.5.0" {
			t.Errorf("the hook saw X-Schemaf-Version %q, want 1.5.0 (schemaf sets it first)", v)
		}
	}
}

// A handler runs after the hook and may override a header the hook set.
func TestHandlerOverridesHookHeader(t *testing.T) {
	withHook(t, func(_ *http.Request, h http.Header) {
		h.Set("X-Frame-Options", "DENY")
	})
	url := headerServer(t, false)
	resp, _ := get(t, "GET", url+"/api/test/frame")
	if got := resp.Header.Values("X-Frame-Options"); len(got) != 1 || got[0] != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want the handler's [SAMEORIGIN]", got)
	}
}

// A hook that sets, changes or deletes an X-Schemaf-* header fails the
// request with a 500, logs the header and the reserved prefix, and the
// handler doesn't run.
func TestResponseHeadersHookReservedPrefix(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", "myapp:1.5.0")
	for _, tc := range []struct {
		name, header string
		hook         ResponseHeadersFunc
	}{
		{"sets another", "X-Schemaf-Build", func(_ *http.Request, h http.Header) { h.Set("X-Schemaf-Build", "7") }},
		{"sets another, raw key", "x-schemaf-build", func(_ *http.Request, h http.Header) { h["x-schemaf-build"] = []string{"7"} }},
		{"changes the version", "X-Schemaf-Version", func(_ *http.Request, h http.Header) { h.Set("X-Schemaf-Version", "9.9.9") }},
		{"adds a version value", "X-Schemaf-Version", func(_ *http.Request, h http.Header) { h.Add("X-Schemaf-Version", "9.9.9") }},
		{"deletes the version", "X-Schemaf-Version", func(_ *http.Request, h http.Header) { h.Del("X-Schemaf-Version") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			withHook(t, tc.hook)
			url := headerServer(t, false)
			resp, body := get(t, "GET", url+"/api/test/typed")
			if resp.StatusCode != 500 {
				t.Fatalf("status = %d, want 500 (body %q)", resp.StatusCode, body)
			}
			if got := resp.Header.Values("X-Schemaf-Version"); len(got) != 1 || got[0] != "1.5.0" {
				t.Errorf("X-Schemaf-Version = %q on the 500, want [1.5.0]", got)
			}
			for k := range resp.Header {
				if hasReservedPrefix(k) && k != "X-Schemaf-Version" {
					t.Errorf("the 500 carries the hook's %s", k)
				}
			}
			logged := buf.String()
			for _, want := range []string{tc.header, "the X-Schemaf- prefix is reserved for schemaf"} {
				if !strings.Contains(logged, want) {
					t.Errorf("log lacks %q:\n%s", want, logged)
				}
			}
		})
	}
}

// The embedded index.html gets the version's meta tag before </head>, at /
// and on the SPA fallback; other files are served as they are.
func TestIndexHTMLMetaTag(t *testing.T) {
	t.Setenv("SCHEMAF_IMAGE", "myapp:3f9a2c1-dirty")
	url := headerServer(t, false)
	want := `<!doctype html><html><head><title>t</title><meta name="schemaf-version" content="3f9a2c1-dirty"></head><body>frontend</body></html>`
	for _, path := range []string{"/", "/some/client/route"} {
		resp, body := get(t, "GET", url+path)
		if body != want {
			t.Errorf("GET %s served %q, want %q", path, body, want)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s Content-Type = %q, want text/html", path, ct)
		}
	}
	resp, body := get(t, "HEAD", url+"/")
	if resp.StatusCode != 200 || body != "" {
		t.Errorf("HEAD / = %d %q, want 200 and no body", resp.StatusCode, body)
	}
	if _, body := get(t, "GET", url+"/app.js"); body != "console.log(1)" {
		t.Errorf("GET /app.js served %q, want the file as it is", body)
	}
}

// An embedded index.html without </head> is a startup error; a frontend
// without index.html is not.
func TestIndexHTMLWithoutHead(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	SetOpenAPISpec(specFor(Routes()))
	SetFrontend(fstest.MapFS{"index.html": {Data: []byte("<html><body>no head</body></html>")}})
	_, err := NewHandler(false)
	if err == nil || !strings.Contains(err.Error(), "index.html has no </head>") {
		t.Errorf("NewHandler = %v, want an error about the missing </head>", err)
	}

	SetFrontend(fstest.MapFS{".gitkeep": {}})
	if _, err := NewHandler(false); err != nil {
		t.Errorf("NewHandler without index.html = %v, want no error", err)
	}
}
