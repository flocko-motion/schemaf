// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// PublicConfigPrefix marks environment variables as public frontend config.
// Every variable with this prefix is served by /api/config; nothing else is.
const PublicConfigPrefix = "PUBLIC_"

// publicConfig returns all PUBLIC_* variables from environ (KEY=VALUE pairs),
// keyed by their full name. Set-but-empty variables are included as "".
func publicConfig(environ []string) map[string]string {
	cfg := map[string]string{}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(k, PublicConfigPrefix) {
			cfg[k] = v
		}
	}
	return cfg
}

// ConfigHandler returns an http.Handler for GET /api/config: a flat JSON
// object of all PUBLIC_* environment variables. The environment is read once,
// when the handler is created (at server start). Always open (no auth).
// Not part of openapi.json; the frontend reads it via the generated config.gen.ts.
func ConfigHandler() http.Handler {
	body, err := json.Marshal(publicConfig(os.Environ()))
	if err != nil {
		panic(err) // map[string]string always marshals
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(body)
	})
}
