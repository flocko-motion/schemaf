package api

import "net/http"

// Route describes a single HTTP route registered with the framework.
type Route struct {
	Method      string       // "GET", "POST", "PUT", "DELETE", etc.
	Path        string       // e.g. "/api/todos", "/api/todos/{id}"
	Auth        bool         // whether JWT auth is required
	Summary     string       // first line of endpoint struct doc comment
	Description string       // remaining lines of endpoint struct doc comment
	Handler     http.Handler
	ReqType     any // pointer to zero-value request type (for OpenAPI reflection)
	RespType    any // pointer to zero-value response type (for OpenAPI reflection)
}

var routes []Route

// Register adds a route to the global registry.
func Register(r Route) {
	routes = append(routes, r)
}

// Routes returns all registered routes.
func Routes() []Route {
	return routes
}
