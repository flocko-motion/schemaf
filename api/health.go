// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"net/http"

	"github.com/flocko-motion/schemaf/db"
)

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
