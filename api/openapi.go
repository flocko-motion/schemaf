// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"errors"
	"net/http"
)

// errNoOpenAPISpec: /openapi.json was requested, but no spec was set.
var errNoOpenAPISpec = errors.New("no OpenAPI spec registered — wire the generated provider in go/main.go: app.AddApi(api.Provider)")

// OpenAPIHandler serves the OpenAPI spec set by SetOpenAPISpec, byte for
// byte: gen/openapi.json as codegen wrote it. There is no spec built at
// runtime. Without a spec it answers 500 and logs why.
func OpenAPIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if openAPISpec == "" {
			writeError(w, r, errNoOpenAPISpec)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAPISpec))
	})
}
