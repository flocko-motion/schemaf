// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/flocko-motion/schemaf/db"
)

var startTime = time.Now()

// StatusProvider returns a value to include in the /api/status response.
type StatusProvider func() any

var statusProviders = map[string]StatusProvider{}

// RegisterStatus adds a named status provider. The returned value is
// included under the given key in the /api/status JSON response.
//
//	api.RegisterStatus("s3", func() any {
//	    return map[string]any{"bucket": "my-bucket", "connected": true}
//	})
func RegisterStatus(name string, fn StatusProvider) {
	statusProviders[name] = fn
}

// StatusHandler returns an http.Handler for GET /api/status.
// Reports service uptime, last backup status, and any registered status providers.
// Always open (no auth required).
func StatusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status": "ok",
			"uptime": time.Since(startTime).Truncate(time.Second).String(),
		}

		if t, errStr := db.LastBackupStatus(); !t.IsZero() {
			backup := map[string]any{
				"last": t.Format(time.RFC3339),
				"ago":  time.Since(t).Truncate(time.Second).String(),
			}
			if errStr != "" {
				backup["error"] = errStr
			}
			resp["backup"] = backup
		}

		for name, fn := range statusProviders {
			resp[name] = fn()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
}
