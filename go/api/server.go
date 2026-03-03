package api

import (
	"net/http"
)

// Serve builds an http.ServeMux with all framework defaults and registered routes,
// then starts the HTTP server on the given address.
// addr should be in the form ":7001".
func Serve(addr string) error {
	mux := http.NewServeMux()

	// Framework default endpoints
	mux.Handle("GET /openapi.json", OpenAPIHandler())
	mux.Handle("GET /openapi.ts", APITSHandler())
	mux.Handle("GET /api/health", HealthHandler())

	// Registered project routes
	for _, r := range Routes() {
		pattern := r.Method + " " + r.Path
		mux.Handle(pattern, r.Handler)
	}

	return http.ListenAndServe(addr, mux)
}
