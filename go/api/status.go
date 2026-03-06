package api

import (
	"encoding/json"
	"net/http"
	"time"
)

var startTime = time.Now()

// StatusHandler returns an http.Handler for GET /api/status.
// Reports service uptime. Always open (no auth required).
func StatusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"uptime": time.Since(startTime).Truncate(time.Second).String(),
		})
	})
}
