// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestStatusFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"bad request", ErrBadRequest, http.StatusBadRequest},
		{"forbidden", ErrForbidden, http.StatusForbidden},
		{"not found", ErrNotFound, http.StatusNotFound},
		{"conflict", ErrConflict, http.StatusConflict},
		{"unavailable", ErrUnavailable, http.StatusServiceUnavailable},
		{"unknown -> 500", errors.New("boom"), http.StatusInternalServerError},
		// Handlers usually wrap sentinels for context; errors.Is must still match.
		{"wrapped conflict", fmt.Errorf("saving order: %w", ErrConflict), http.StatusConflict},
		{"wrapped unavailable", fmt.Errorf("db down: %w", ErrUnavailable), http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		if got := statusFor(c.err); got != c.want {
			t.Errorf("%s: statusFor = %d, want %d", c.name, got, c.want)
		}
	}
}
