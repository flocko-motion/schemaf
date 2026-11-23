// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import "net/http"

// Route describes a single HTTP route registered with the framework.
type Route struct {
	Method      string    // "GET", "POST", "PUT", "DELETE", etc.
	Path        string    // e.g. "/api/todos", "/api/todos/{id}"
	Auth        AuthLevel // AuthNone, AuthOptional or AuthRequired
	Summary     string    // first line of endpoint struct doc comment
	Description string    // remaining lines of endpoint struct doc comment
	Handler     http.Handler
}

var routes []Route

// openAPISpec is the spec served at /openapi.json, set by the generated
// Provider() (go/api/endpoints.gen.go).
var openAPISpec string

// SetOpenAPISpec sets the OpenAPI spec served at /openapi.json. The
// generated Provider() calls it with gen/openapi.json, so the server serves
// exactly the spec the clients were generated from. Not for hand-written
// code.
func SetOpenAPISpec(spec string) {
	openAPISpec = spec
}

// Register adds a route to the global registry.
func Register(r Route) {
	routes = append(routes, r)
}

// Routes returns all registered routes.
func Routes() []Route {
	return routes
}

// Reset clears everything a server start sets: the routes and the spec, the
// embedded frontend, the signing key, the session store, the identity
// providers, the OnLogin and OnRequest hooks, the CanSeeErrorDetails
// predicate and the updates authorizer. The error details store is kept: WriteError records into it
// outside any server too. The server calls it before it registers the
// routes and after it stops, so every start begins from nothing.
func Reset() {
	routes = nil
	openAPISpec = ""
	frontendFS = nil
	signingKey = nil
	sessionStore = nil
	identityProviders = nil
	onLogin = nil
	onRequest = nil
	onResponseHeaders = nil
	canSeeErrorDetails = nil
	updatesAuth = nil
}
