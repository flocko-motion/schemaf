// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"net/http"

	"github.com/flocko-motion/schemaf/db"
)

// HealthChecker returns nil if healthy, or an error describing the problem.
type HealthChecker func() error

var healthCheckers = map[string]HealthChecker{}

// RegisterHealth adds a named health check. If the checker returns an error,
// the overall health status is "error" and the response code is 503.
//
//	api.RegisterHealth("s3", func() error {
//	    return s3Client.Ping()
//	})
func RegisterHealth(name string, fn HealthChecker) {
	healthCheckers[name] = fn
}

// HealthHandler returns an http.Handler that reports the health of the service and database.
// GET /api/health → {"status":"ok","db":"ok"} or {"status":"error","db":"error: ..."}
func HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{}
		overallOK := true

		if db.DB() == nil {
			resp["db"] = "not configured"
		} else if err := db.HealthCheck(); err != nil {
			resp["db"] = "error: " + err.Error()
			overallOK = false
		} else {
			resp["db"] = "ok"
		}

		for name, fn := range healthCheckers {
			if err := fn(); err != nil {
				resp[name] = "error: " + err.Error()
				overallOK = false
			} else {
				resp[name] = "ok"
			}
		}

		if overallOK {
			resp["status"] = "ok"
		} else {
			resp["status"] = "error"
		}

		w.Header().Set("Content-Type", "application/json")
		if !overallOK {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(resp)
	})
}
