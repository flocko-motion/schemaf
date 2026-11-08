// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"net/http"
)

// UserMeHandler returns an http.Handler for GET /api/user/me (AuthRequired):
// the authenticated caller's subject.
func UserMeHandler() http.Handler {
	return withAuth(AuthRequired, userMeHandler())
}

func userMeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub, _ := Subject(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": sub})
	})
}
