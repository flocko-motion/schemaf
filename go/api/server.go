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
