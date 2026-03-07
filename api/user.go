package api

import (
	"encoding/json"
	"net/http"
)

// UserMeHandler returns an http.Handler for GET /api/user/me.
// Requires a valid JWT — returns the authenticated user's UUID from the sub claim.
func UserMeHandler() http.Handler {
	return requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub, _ := Subject(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": sub})
	}))
}
