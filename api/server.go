// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"net/http"
)

// NewMux builds and returns the http.ServeMux with all framework defaults and
// registered routes. Useful for testing with httptest.NewServer.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /openapi.json", OpenAPIHandler())
	mux.Handle("GET /api/health", HealthHandler())
	mux.Handle("GET /api/status", StatusHandler())
	mux.Handle("GET /api/user/me", UserMeHandler())
	for _, r := range Routes() {
		mux.Handle(r.Method+" "+r.Path, r.Handler)
	}
	return mux
}

// Serve starts the HTTP server on the given address using the registered routes.
// addr should be in the form ":7001".
func Serve(addr string) error {
	return http.ListenAndServe(addr, NewMux())
}
