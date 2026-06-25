// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"errors"
	"net/http"
)

// Sentinel errors that handlers can return (directly or wrapped with %w) to
// produce specific HTTP status codes.
var (
	ErrBadRequest  = errors.New("bad request") // 400
	ErrForbidden   = errors.New("forbidden")   // 403
	ErrNotFound    = errors.New("not found")   // 404
	ErrConflict    = errors.New("conflict")    // 409
	ErrUnavailable = errors.New("unavailable") // 503
)

// statusFor maps a handler error to an HTTP status code. Unrecognized errors
// map to 500.
func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrBadRequest):
		return http.StatusBadRequest
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
