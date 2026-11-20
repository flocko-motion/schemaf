// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"fmt"
	"net/http"
	"strings"
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
		{openAPIPattern, AuthNone, OpenAPIHandler()},
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
//
// The OpenAPI spec set by SetOpenAPISpec gates /api and everything under
// /api/: a path not in the spec is a 404, a method not in the spec a 405
// (see EXTEND.md, Routing). NewMux panics, as http.ServeMux does on
// conflicting patterns, if the spec and the handlers don't agree (see
// checkRoutes); Serve returns that error instead.
func NewMux() *http.ServeMux {
	mux, err := buildMux()
	if err != nil {
		panic(err)
	}
	return mux
}

// buildMux builds the mux behind NewMux and Serve.
func buildMux() (*http.ServeMux, error) {
	if openAPISpec == "" {
		return nil, fmt.Errorf("api: %w", errNoOpenAPISpec)
	}
	table, err := parseSpec(openAPISpec)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	var handlers []handlerRoute
	for _, fr := range frameworkRoutes() {
		if fr.pattern == openAPIPattern {
			continue
		}
		method, path, _ := strings.Cut(fr.pattern, " ")
		handlers = append(handlers, handlerRoute{method, path})
	}
	for _, r := range Routes() {
		handlers = append(handlers, handlerRoute{r.Method, r.Path})
	}
	if err := checkRoutes(table, handlers); err != nil {
		return nil, err
	}

	// The API: behind the gate, so only what the spec declares reaches a
	// handler (and its auth).
	api := http.NewServeMux()
	for _, fr := range frameworkRoutes() {
		if fr.pattern == openAPIPattern {
			continue
		}
		api.Handle(fr.pattern, withAuth(fr.auth, fr.handler))
	}
	for _, r := range Routes() {
		api.Handle(r.Method+" "+r.Path, r.Handler)
	}
	gated := gate(table, api)

	mux := http.NewServeMux()
	mux.Handle(openAPIPattern, OpenAPIHandler())
	mux.Handle("/api", gated)
	mux.Handle(apiPrefix, gated)
	// Everything else is the frontend (dev proxy or embedded static files),
	// for GET and HEAD only.
	mux.Handle("/", frontendOnly(frontendHandler()))
	return mux, nil
}

// openAPIPattern is the one route outside /api/.
const openAPIPattern = "GET /openapi.json"

// Serve starts the HTTP server on the given address using the registered
// routes. addr should be in the form ":8000". It returns NewMux's startup
// error instead of panicking.
func Serve(addr string) error {
	mux, err := buildMux()
	if err != nil {
		return err
	}
	return http.ListenAndServe(addr, mux)
}
