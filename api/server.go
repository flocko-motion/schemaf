// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"net/http"
)

// frameworkRoute is a built-in route with its explicit auth level.
type frameworkRoute struct {
	pattern string
	auth    AuthLevel
	handler http.Handler
}

// frameworkRoutes lists the built-in routes. POST /api/auth/logout is
// AuthNone because it authenticates itself: it ends the session if there is
// one, always clears the cookie, and applies the Origin check to cookie
// requests.
func frameworkRoutes() []frameworkRoute {
	return []frameworkRoute{
		{"GET /openapi.json", AuthNone, OpenAPIHandler()},
		{"GET /api/health", AuthNone, HealthHandler()},
		{"GET /api/status", AuthNone, StatusHandler()},
		{"GET /api/config", AuthNone, ConfigHandler()},
		{"GET /api/user/me", AuthRequired, userMeHandler()},
		{"POST /api/auth/{provider}/login", AuthNone, loginHandler()},
		{"POST /api/auth/logout", AuthNone, logoutHandler()},
		{"POST /api/auth/refresh", AuthRequired, refreshHandler()},
	}
}

// NewMux builds and returns the http.ServeMux with all framework defaults and
// registered routes. Useful for testing with httptest.NewServer.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	for _, fr := range frameworkRoutes() {
		mux.Handle(fr.pattern, withAuth(fr.auth, fr.handler))
	}
	for _, r := range Routes() {
		mux.Handle(r.Method+" "+r.Path, r.Handler)
	}

	// Catch-all: serve frontend (dev proxy or embedded static files).
	mux.Handle("/", frontendHandler())

	return mux
}

// Serve starts the HTTP server on the given address using the registered routes.
// addr should be in the form ":8000".
func Serve(addr string) error {
	return http.ListenAndServe(addr, NewMux())
}
