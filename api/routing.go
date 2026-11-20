// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// apiPrefix is the path space the OpenAPI spec gates. Every route lives
// under it, except GET /openapi.json.
const apiPrefix = "/api/"

// specPath is one path of the OpenAPI spec and its methods.
type specPath struct {
	template string          // e.g. "/api/todos/{id}"
	segs     []string        // template split at "/"
	methods  map[string]bool // upper case, e.g. "GET"
}

// specTable is the paths and methods of the spec the server serves. It gates
// every request to /api and under /api/ (see gate).
type specTable []specPath

// httpMethods are the keys of an OpenAPI path item that are operations.
var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// parseSpec reads the paths and methods of an OpenAPI spec.
func parseSpec(spec string) (specTable, error) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(spec), &doc); err != nil {
		return nil, fmt.Errorf("the OpenAPI spec is not valid JSON: %w", err)
	}
	var table specTable
	var problems []string
	for tmpl, item := range doc.Paths {
		// Codegen never writes either; the gate can't judge such a path.
		if _, ok := item["$ref"]; ok {
			problems = append(problems, fmt.Sprintf("OpenAPI spec path %s: $ref path items aren't supported", tmpl))
			continue
		}
		p := specPath{template: tmpl, segs: strings.Split(tmpl, "/"), methods: map[string]bool{}}
		for key := range item {
			if slices.Contains(httpMethods, key) {
				p.methods[strings.ToUpper(key)] = true
			}
		}
		if len(p.methods) == 0 {
			problems = append(problems, fmt.Sprintf("OpenAPI spec path %s: declares no operation", tmpl))
			continue
		}
		table = append(table, p)
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, fmt.Errorf("the OpenAPI spec isn't one codegen writes (run ./schemaf.sh codegen):\n  - %s", strings.Join(problems, "\n  - "))
	}
	slices.SortFunc(table, func(a, b specPath) int { return strings.Compare(a.template, b.template) })
	return table, nil
}

// UnsupportedWildcard returns the first segment of a route path that OpenAPI
// can't express: Go's multi-segment wildcard {name...} or its end anchor
// {$}. In OpenAPI a parameter is always exactly one segment, so a route
// using them can't be described truthfully in the spec. Codegen rejects
// them in Path(); the server refuses to start with them (see checkRoutes).
func UnsupportedWildcard(path string) (seg string, found bool) {
	for _, s := range strings.Split(path, "/") {
		if s == "{$}" || (isParam(s) && strings.HasSuffix(s, "...}")) {
			return s, true
		}
	}
	return "", false
}

// isParam reports whether a path segment is a {param}.
func isParam(seg string) bool {
	return len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

// requestSegments splits the request path into unescaped segments, as
// ServeMux does: split the escaped path at "/", then unescape each segment,
// so an encoded slash stays inside its segment.
func requestSegments(r *http.Request) []string {
	segs := strings.Split(r.URL.EscapedPath(), "/")
	for i, s := range segs {
		if u, err := url.PathUnescape(s); err == nil {
			segs[i] = u
		}
	}
	return segs
}

// matches reports whether request path segments match the template: same
// number of segments, literals equal, a {param} matching any non-empty
// segment.
func (p specPath) matches(segs []string) bool {
	if len(segs) != len(p.segs) {
		return false
	}
	for i, s := range p.segs {
		if isParam(s) {
			if segs[i] == "" {
				return false
			}
		} else if s != segs[i] {
			return false
		}
	}
	return true
}

// allowed returns the methods the spec allows for request path segments
// (HEAD wherever GET is), sorted, and whether the path is in the spec at all.
func (t specTable) allowed(segs []string) (methods []string, found bool) {
	set := map[string]bool{}
	for _, p := range t {
		if p.matches(segs) {
			found = true
			for m := range p.methods {
				set[m] = true
			}
		}
	}
	if set["GET"] {
		set["HEAD"] = true
	}
	for m := range set {
		methods = append(methods, m)
	}
	slices.Sort(methods)
	return methods, found
}

// gate lets a request through to next only if its path and method are in
// the spec: an unknown path is a 404, a known path with another method a
// 405 with Allow. It runs before auth.
func gate(table specTable, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods, found := table.allowed(requestSegments(r))
		if !found {
			writeError(w, r, Error(ErrNotFound, "not_found", "not found"))
			return
		}
		if !slices.Contains(methods, r.Method) {
			writeMethodNotAllowed(w, methods)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeMethodNotAllowed writes the 405 for a method outside allow.
func writeMethodNotAllowed(w http.ResponseWriter, allow []string) {
	w.Header().Set("Allow", strings.Join(allow, ", "))
	writeStatusError(w, statusMethodNotAllowed, "method not allowed")
}

// frontendOnly lets only GET and HEAD through to the frontend; every other
// method is a 405.
func frontendOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeMethodNotAllowed(w, []string{"GET", "HEAD"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handlerRoute is a registered handler's method and path, for checkRoutes.
type handlerRoute struct {
	method, path string
}

func (h handlerRoute) String() string { return h.method + " " + h.path }

// routeKey is the route with its {param} names dropped: two handlers with
// the same key serve the same requests.
func (h handlerRoute) routeKey() string {
	segs := strings.Split(h.path, "/")
	for i, s := range segs {
		if isParam(s) {
			segs[i] = "{}"
		}
	}
	return h.method + " " + strings.Join(segs, "/")
}

// covers reports whether the handler serves the spec operation: same
// method (GET also serving HEAD), and each segment equal or a handler
// {param} (which serves any segment).
func (h handlerRoute) covers(method string, p specPath) bool {
	if h.method != method && !(h.method == http.MethodGet && method == http.MethodHead) {
		return false
	}
	segs := strings.Split(h.path, "/")
	if len(segs) != len(p.segs) {
		return false
	}
	for i, s := range segs {
		if !isParam(s) && s != p.segs[i] {
			return false
		}
	}
	return true
}

// declaredBy reports whether the spec path declares everything the handler
// serves: the method, and each segment equal or a spec {param} (a handler
// {param} needs a spec {param}).
func (h handlerRoute) declaredBy(p specPath) bool {
	if !p.methods[h.method] {
		return false
	}
	segs := strings.Split(h.path, "/")
	if len(segs) != len(p.segs) {
		return false
	}
	for i, s := range segs {
		if isParam(p.segs[i]) {
			continue
		}
		if isParam(s) || s != p.segs[i] {
			return false
		}
	}
	return true
}

// checkRoutes checks that the spec and the handlers agree: every handler is
// under /api/ and declared in the spec (else the gate makes it unreachable),
// and every spec operation has a handler. It names every mismatch.
func checkRoutes(table specTable, handlers []handlerRoute) error {
	var problems []string
	seen := map[string]bool{}
	for _, h := range handlers {
		// ServeMux panics on two patterns matching the same requests; report
		// it here so NewHandler returns an error.
		if key := h.routeKey(); seen[key] {
			problems = append(problems, fmt.Sprintf("%s: more than one handler for this route (an endpoint on one of schemaf's own routes?)", h))
			continue
		} else {
			seen[key] = true
		}
		if seg, ok := UnsupportedWildcard(h.path); ok {
			problems = append(problems, fmt.Sprintf("%s: uses %s, which OpenAPI can't express — use one {param} per segment", h, seg))
			continue
		}
		if !strings.HasPrefix(h.path, apiPrefix) {
			problems = append(problems, fmt.Sprintf("%s: handler registered outside /api/ — every route lives under /api/ (only schemaf's %s is outside)", h, openAPIPattern))
			continue
		}
		if !slices.ContainsFunc(table, h.declaredBy) {
			problems = append(problems, fmt.Sprintf("%s: handler has no operation in the OpenAPI spec, so it is unreachable", h))
		}
	}
	for _, p := range table {
		for _, m := range slices.Sorted(maps.Keys(p.methods)) {
			if !slices.ContainsFunc(handlers, func(h handlerRoute) bool { return h.covers(m, p) }) {
				problems = append(problems, fmt.Sprintf("%s %s: OpenAPI spec operation has no handler", m, p.template))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	slices.Sort(problems)
	return fmt.Errorf("api: the OpenAPI spec and the registered handlers don't agree (run ./schemaf.sh codegen; hand-registered routes need a spec operation):\n  - %s",
		strings.Join(problems, "\n  - "))
}
