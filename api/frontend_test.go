package api

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/flocko-motion/schemaf/constants"
)

// TestFrontendFollowsDevMode: the server proxies to the Vite dev server only in
// development mode; otherwise it serves the embedded frontend.
func TestFrontendFollowsDevMode(t *testing.T) {
	// A stand-in for the Vite dev server, on port+2 of a base port we pick.
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "vite")
	}))
	defer vite.Close()
	_, portStr, _ := net.SplitHostPort(vite.Listener.Addr().String())
	vitePort, _ := strconv.Atoi(portStr)
	oldPort := constants.Port()
	constants.SetPort(vitePort - 2)
	defer constants.SetPort(oldPort)

	oldFS := frontendFS
	SetFrontend(fstest.MapFS{"index.html": {Data: []byte("<head></head>embedded")}})
	defer SetFrontend(oldFS)

	for _, tc := range []struct{ dev, want string }{
		{dev: "1", want: "vite"},
		{dev: "", want: `<head><meta name="schemaf-version" content="dev"></head>embedded`},
	} {
		t.Run("SCHEMAF_DEV="+tc.dev, func(t *testing.T) {
			t.Setenv("SCHEMAF_DEV", tc.dev)
			t.Setenv("SCHEMAF_ENV", "") // native, outside compose: must not decide the mode
			rec := httptest.NewRecorder()
			h, err := frontendHandler(DevMode(), "dev")
			if err != nil {
				t.Fatal(err)
			}
			h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
			if got := rec.Body.String(); got != tc.want {
				t.Errorf("GET / served %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSecureCookiesOutsideDevMode: session cookies carry Secure in production,
// including a native server outside compose.
func TestSecureCookiesOutsideDevMode(t *testing.T) {
	t.Setenv("SCHEMAF_ENV", "")
	t.Setenv("SCHEMAF_DEV", "")
	if !secureCookies() {
		t.Error("production: secureCookies() = false, want true")
	}
	t.Setenv("SCHEMAF_DEV", "1")
	if secureCookies() {
		t.Error("development: secureCookies() = true, want false")
	}
}
