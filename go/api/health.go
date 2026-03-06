package api

import (
	"encoding/json"
	"net/http"

	"schemaf.local/base/db"
)

// HealthHandler returns an http.Handler that reports the health of the service and database.
// GET /api/health → {"status":"ok","db":"ok"} or {"status":"error","db":"error: ..."}
func HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{}
		overallOK := true

		if err := db.DB().PingContext(r.Context()); err != nil {
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
