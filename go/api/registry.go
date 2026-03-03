package api

import "net/http"

// Route describes a single HTTP route to be registered with the framework.
type Route struct {
	Method   string // "GET", "POST", "PUT", "DELETE", etc.
	Path     string // e.g. "/api/todos", "/api/todos/{id}"
	Summary  string // human-readable; appears in OpenAPI
	Handler  http.Handler
	ReqType  any // pointer to request body type (for reflection); optional
	RespType any // pointer to response type (for reflection); optional
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
