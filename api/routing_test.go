package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flocko-motion/schemaf/constants"
)

// specFor returns an OpenAPI spec as codegen builds it: the framework's
// routes plus one operation per route.
func specFor(routes []Route) string {
	paths, _ := FrameworkOpenAPI()
	for _, r := range routes {
		item, _ := paths[r.Path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[r.Path] = item
		}
		item[strings.ToLower(r.Method)] = map[string]any{"responses": map[string]any{}}
	}
	b, err := json.Marshal(map[string]any{"openapi": "3.0.0", "paths": paths})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// testMux sets the spec codegen would generate for the registered routes and
// returns NewMux(), as the generated Provider() and the server do.
func testMux(t *testing.T) http.Handler {
	t.Helper()
	SetOpenAPISpec(specFor(Routes()))
	return NewMux()
}

// frontendModes runs fn once with the embedded production frontend and once
// with the dev proxy (to a stand-in Vite server). Both answer with a body
// that contains "frontend".
func frontendModes(t *testing.T, fn func(t *testing.T)) {
	t.Helper()
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "frontend")
	}))
	t.Cleanup(vite.Close)
	_, portStr, _ := net.SplitHostPort(vite.Listener.Addr().String())
	vitePort, _ := strconv.Atoi(portStr)
	oldPort := constants.Port()
	constants.SetPort(vitePort - 2)
	t.Cleanup(func() { constants.SetPort(oldPort) })

	oldFS := frontendFS
	SetFrontend(fstest.MapFS{"index.html": {Data: []byte("<head></head>frontend")}})
	t.Cleanup(func() { SetFrontend(oldFS) })

	for _, mode := range []struct{ name, dev string }{{"prod", ""}, {"dev", "1"}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Setenv("SCHEMAF_DEV", mode.dev)
			t.Setenv("SCHEMAF_ENV", "")
			fn(t)
		})
	}
}

// serveMux answers one request on a fresh test server for the registered routes.
func serveMux(t *testing.T, method, path string) (*http.Response, string) {
	t.Helper()
	srv := httptest.NewServer(testMux(t))
	t.Cleanup(srv.Close)
	return do(t, method, srv.URL+path, "{}", nil)
}

// wantJSONError checks status, the JSON error body and its code.
func wantJSONError(t *testing.T, resp *http.Response, body string, status int, msg, code string) {
	t.Helper()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, status, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got errorResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q is not JSON: %v", body, err)
	}
	if got.Error != msg || got.Code != code {
		t.Errorf("body = %q, want error %q, code %q", body, msg, code)
	}
}

// TestGateUnknownAPIPath: a path under /api/ that isn't in the spec is a JSON
// 404 for every method, never the frontend.
func TestGateUnknownAPIPath(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	frontendModes(t, func(t *testing.T) {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			for _, path := range []string{"/api/does-not-exist", "/api", "/api/health/"} {
				resp, body := serveMux(t, method, path)
				wantJSONError(t, resp, body, 404, "not found", "not_found")
			}
		}
	})
}

// TestGateWrongMethod: a path in the spec with a method that isn't is a JSON
// 405 whose Allow lists the spec's methods, HEAD wherever GET is.
func TestGateWrongMethod(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	frontendModes(t, func(t *testing.T) {
		resp, body := serveMux(t, "POST", "/api/health")
		wantJSONError(t, resp, body, 405, "method not allowed", "method_not_allowed")
		if got := resp.Header.Get("Allow"); got != "GET, HEAD" {
			t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
		}
		resp, body = serveMux(t, "GET", "/api/auth/logout")
		wantJSONError(t, resp, body, 405, "method not allowed", "method_not_allowed")
		if got := resp.Header.Get("Allow"); got != "POST" {
			t.Errorf("Allow = %q, want %q", got, "POST")
		}
	})
}

// TestFrontendSpaceGetHeadOnly: outside /api/ only GET and HEAD reach the
// frontend; /openapi.json still answers.
func TestFrontendSpaceGetHeadOnly(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	frontendModes(t, func(t *testing.T) {
		for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
			resp, body := serveMux(t, method, "/some-page")
			wantJSONError(t, resp, body, 405, "method not allowed", "method_not_allowed")
			if got := resp.Header.Get("Allow"); got != "GET, HEAD" {
				t.Errorf("%s: Allow = %q, want %q", method, got, "GET, HEAD")
			}
		}
		resp, body := serveMux(t, "GET", "/some-page")
		if resp.StatusCode != 200 || !strings.Contains(body, "frontend") {
			t.Errorf("GET /some-page = %d %q, want 200 from the frontend", resp.StatusCode, body)
		}
		resp, _ = serveMux(t, "HEAD", "/some-page")
		if resp.StatusCode != 200 {
			t.Errorf("HEAD /some-page = %d, want 200", resp.StatusCode)
		}
		resp, body = serveMux(t, "GET", "/openapi.json")
		if resp.StatusCode != 200 || !strings.Contains(body, `"/api/health"`) {
			t.Errorf("GET /openapi.json = %d %q, want the spec", resp.StatusCode, body)
		}
		resp, body = serveMux(t, "POST", "/openapi.json")
		wantJSONError(t, resp, body, 405, "method not allowed", "method_not_allowed")
	})
}

// pathEndpoint is a typed endpoint at any method and path.
type pathEndpoint struct{ method, path string }

func (e pathEndpoint) Method() string  { return e.method }
func (e pathEndpoint) Path() string    { return e.path }
func (e pathEndpoint) Auth() AuthLevel { return AuthNone }
func (e pathEndpoint) Handle(context.Context, struct{}) (struct{}, error) {
	return struct{}{}, nil
}

// startupError returns NewMux's panic and Serve's error, and checks they agree.
func startupError(t *testing.T) string {
	t.Helper()
	var panicked any
	func() {
		defer func() { panicked = recover() }()
		NewMux()
	}()
	if panicked == nil {
		t.Fatal("NewMux did not panic")
	}
	// NewHandler returns the same error instead of panicking.
	_, serveErr := NewHandler(false)
	if serveErr == nil || serveErr.Error() != fmt.Sprint(panicked) {
		t.Fatalf("NewHandler returned %v, want NewMux's error %q", serveErr, panicked)
	}
	return serveErr.Error()
}

// TestStartupCheck: the server refuses to start, naming every mismatch,
// when the spec and the handlers don't agree.
func TestStartupCheck(t *testing.T) {
	cases := []struct {
		name   string
		routes []Route       // registered
		spec   func() string // the spec that is set
		want   []string      // in the error
	}{
		{"no spec", nil, func() string { return "" },
			[]string{"no OpenAPI spec registered", "app.AddApi(api.Provider)"}},
		{"handler without spec operation",
			[]Route{NewRoute[struct{}, struct{}](pathEndpoint{"GET", "/api/orphan"}, "", ""),
				NewRoute[struct{}, struct{}](pathEndpoint{"DELETE", "/api/orphan2/{id}"}, "", "")},
			func() string { return specFor(nil) },
			[]string{"GET /api/orphan", "DELETE /api/orphan2/{id}", "no operation in the OpenAPI spec"}},
		{"spec operation without handler", nil,
			func() string {
				return specFor([]Route{{Method: "GET", Path: "/api/ghost"}, {Method: "PUT", Path: "/api/health"}})
			},
			[]string{"GET /api/ghost", "PUT /api/health", "no handler"}},
		{"route outside /api/",
			[]Route{NewRoute[struct{}, struct{}](pathEndpoint{"POST", "/webhooks/x"}, "", "")},
			func() string { return specFor(Routes()) },
			[]string{"POST /webhooks/x", "outside /api/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Reset()
			t.Cleanup(Reset)
			for _, r := range tc.routes {
				Register(r)
			}
			SetOpenAPISpec(tc.spec())
			msg := startupError(t)
			for _, w := range tc.want {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q\nwant it to contain %q", msg, w)
				}
			}
		})
	}
}

// TestAppEndpointInSpec: an app endpoint in the spec is served, with its
// path parameters.
func TestAppEndpointInSpec(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	Register(NewRoute[struct{}, struct{}](pathEndpoint{"DELETE", "/api/things/{id}"}, "", ""))
	resp, body := serveMux(t, "DELETE", "/api/things/7")
	if resp.StatusCode != 200 {
		t.Errorf("DELETE /api/things/7 = %d %q, want 200", resp.StatusCode, body)
	}
	resp, body = serveMux(t, "GET", "/api/things/7")
	wantJSONError(t, resp, body, 405, "method not allowed", "method_not_allowed")
	if got := resp.Header.Get("Allow"); got != "DELETE" {
		t.Errorf("Allow = %q, want DELETE", got)
	}
}

// TestGateEscapedSegment: the gate matches segments as ServeMux does, so an
// encoded slash inside a path parameter stays one segment.
func TestGateEscapedSegment(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	Register(NewRoute[struct{}, struct{}](pathEndpoint{"GET", "/api/files/{name}"}, "", ""))
	resp, body := serveMux(t, "GET", "/api/files/report%2F2026.pdf")
	if resp.StatusCode != 200 {
		t.Errorf("GET /api/files/report%%2F2026.pdf = %d %q, want 200", resp.StatusCode, body)
	}
}

// TestStartupCheckDuplicateRoute: two handlers on the same route are a
// startup error naming the route, not a panic from ServeMux inside Serve.
func TestStartupCheckDuplicateRoute(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	Register(NewRoute[struct{}, struct{}](pathEndpoint{"GET", "/api/status"}, "", ""))
	Register(NewRoute[struct{}, struct{}](pathEndpoint{"GET", "/api/x/{a}"}, "", ""))
	Register(NewRoute[struct{}, struct{}](pathEndpoint{"GET", "/api/x/{b}"}, "", ""))
	SetOpenAPISpec(specFor(Routes()))
	msg := startupError(t)
	for _, w := range []string{"GET /api/status: more than one handler", "GET /api/x/{b}: more than one handler"} {
		if !strings.Contains(msg, w) {
			t.Errorf("error %q\nwant it to contain %q", msg, w)
		}
	}
}

// TestStartupCheckSpecShape: wildcards OpenAPI can't express, $ref path
// items and path items without operations refuse to start.
func TestStartupCheckSpecShape(t *testing.T) {
	cases := []struct {
		name   string
		routes []Route
		extra  string // raw JSON path items added to the spec's paths
		want   []string
	}{
		{"handler with {name...}",
			[]Route{NewRoute[struct{}, struct{}](pathEndpoint{"GET", "/api/blob/{path...}"}, "", "")}, "",
			[]string{"GET /api/blob/{path...}: uses {path...}, which OpenAPI can't express"}},
		{"handler with {$}",
			[]Route{NewRoute[struct{}, struct{}](pathEndpoint{"GET", "/api/blob/{$}"}, "", "")}, "",
			[]string{"GET /api/blob/{$}: uses {$}, which OpenAPI can't express"}},
		{"$ref path item", nil, `"/api/x": {"$ref": "#/components/pathItems/X"}`,
			[]string{"OpenAPI spec path /api/x: $ref path items aren't supported"}},
		{"path item without operations", nil, `"/api/y": {"summary": "nothing"}`,
			[]string{"OpenAPI spec path /api/y: declares no operation"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Reset()
			t.Cleanup(Reset)
			for _, r := range tc.routes {
				Register(r)
			}
			spec := specFor(Routes())
			if tc.extra != "" {
				spec = strings.Replace(spec, `"paths":{`, `"paths":{`+tc.extra+`,`, 1)
			}
			SetOpenAPISpec(spec)
			msg := startupError(t)
			for _, w := range tc.want {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q\nwant it to contain %q", msg, w)
				}
			}
		})
	}
}
