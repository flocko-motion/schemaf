// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/flocko-motion/schemaf/log"
)

// Sentinel errors that handlers can return (directly or wrapped with %w) to
// produce specific HTTP status codes. See EXTEND.md, "Error responses".
var (
	ErrBadRequest      = errors.New("bad request")       // 400
	ErrUnauthorized    = errors.New("unauthorized")      // 401
	ErrForbidden       = errors.New("forbidden")         // 403
	ErrNotFound        = errors.New("not found")         // 404
	ErrConflict        = errors.New("conflict")          // 409
	ErrTooManyRequests = errors.New("too many requests") // 429
	ErrUnavailable     = errors.New("unavailable")       // 503
)

// sentinel is a schemaf sentinel error with its status and default code.
type sentinel struct {
	err    error
	name   string // for misuse messages
	status int
	code   string
}

var sentinels = []sentinel{
	{ErrBadRequest, "api.ErrBadRequest", http.StatusBadRequest, "bad_request"},
	{ErrUnauthorized, "api.ErrUnauthorized", http.StatusUnauthorized, "unauthorized"},
	{ErrForbidden, "api.ErrForbidden", http.StatusForbidden, "forbidden"},
	{ErrNotFound, "api.ErrNotFound", http.StatusNotFound, "not_found"},
	{ErrConflict, "api.ErrConflict", http.StatusConflict, "conflict"},
	{ErrTooManyRequests, "api.ErrTooManyRequests", http.StatusTooManyRequests, "too_many_requests"},
	{ErrUnavailable, "api.ErrUnavailable", http.StatusServiceUnavailable, "unavailable"},
}

// codeInternal is the default code of a 500 (an error without a sentinel).
const codeInternal = "internal"

// statusFor maps a handler error to an HTTP status code: the status of the
// first sentinel in the chain (errors.Is), 500 without one.
func statusFor(err error) int {
	for _, s := range sentinels {
		if errors.Is(err, s.err) {
			return s.status
		}
	}
	return http.StatusInternalServerError
}

// defaultCode returns the default code of a status.
func defaultCode(status int) string {
	for _, s := range sentinels {
		if s.status == status {
			return s.code
		}
	}
	return codeInternal
}

// codedError is the error api.Error returns.
type codedError struct {
	sentinel error
	code     string
	msg      string
}

func (e *codedError) Error() string { return e.msg }
func (e *codedError) Unwrap() error { return e.sentinel }
func (e *codedError) Code() string  { return e.code }

// Error returns an error that answers with the sentinel's status, the given
// code and the given message:
//
//	return Resp{}, api.Error(api.ErrConflict, "last_head", "an organisation needs at least one head")
//	// → 409 {"error": "an organisation needs at least one head", "code": "last_head"}
//
// Its Error() is msg, it unwraps to sentinel and its Code() is code. Codes
// match ^[a-z][a-z0-9_]*$; a default code is only valid with its own status.
// Misuse answers with a 500 (code "internal") and is logged.
func Error(sentinel error, code, msg string) error {
	return &codedError{sentinel: sentinel, code: code, msg: msg}
}

// retryAfterError is the error api.RetryAfter returns.
type retryAfterError struct {
	err error
	d   time.Duration
}

func (e *retryAfterError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}
func (e *retryAfterError) Unwrap() error             { return e.err }
func (e *retryAfterError) RetryAfter() time.Duration { return e.d }

// RetryAfter wraps err so its response carries a Retry-After header of d, in
// whole seconds rounded up. Only valid on 429 and 503 with d > 0; otherwise
// the response is a 500 (code "internal") and the misuse is logged.
//
//	return Resp{}, api.RetryAfter(
//	    api.Error(api.ErrTooManyRequests, "token_locked", "too many failed attempts, try again later"),
//	    90*time.Second)
func RetryAfter(err error, d time.Duration) error {
	return &retryAfterError{err: err, d: d}
}

// errorResponse is the body of every error response.
type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
	ID    string `json:"id,omitempty"`
}

// resolvedError is what an error answers with.
type resolvedError struct {
	status     int
	code       string
	msg        string
	retryAfter time.Duration // 0: no Retry-After header
	misuse     string        // non-empty: err misuses the error API (problem and fix)
}

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// sentinelNames lists the sentinels for misuse messages.
var sentinelNames = func() string {
	names := make([]string, len(sentinels))
	for i, s := range sentinels {
		names[i] = s.name
	}
	return "one of " + strings.Join(names, ", ")
}()

// resolveError derives status, code, message and Retry-After from err. A
// misuse of the error API resolves to a mute 500 with code "internal".
func resolveError(err error) resolvedError {
	res := resolvedError{status: statusFor(err)}
	res.code = defaultCode(res.status)
	res.misuse = checkError(err, &res)
	if res.misuse != "" {
		return resolvedError{
			status: http.StatusInternalServerError,
			code:   codeInternal,
			msg:    http.StatusText(http.StatusInternalServerError),
			misuse: res.misuse,
		}
	}
	res.msg = http.StatusText(res.status)
	if res.status < 500 {
		if m := err.Error(); m != "" {
			res.msg = m
		}
	}
	return res
}

// checkError fills res.code and res.retryAfter from err's Code() and
// RetryAfter() hooks and returns the misuse, if any.
func checkError(err error, res *resolvedError) string {
	var ce *codedError
	if errors.As(err, &ce) {
		if ce.sentinel == nil {
			return fmt.Sprintf("api.Error(nil, %q, …) has a nil sentinel; pass %s", ce.code, sentinelNames)
		}
		if !isSentinel(ce.sentinel) {
			return fmt.Sprintf("api.Error(%q, %q, …): %q is not a schemaf sentinel; pass %s", ce.sentinel, ce.code, ce.sentinel, sentinelNames)
		}
	}

	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		code := coded.Code()
		if !codePattern.MatchString(code) {
			return fmt.Sprintf("invalid code %q from %T: codes must match ^[a-z][a-z0-9_]*$", code, coded)
		}
		if isDefaultCode(code) && code != res.code {
			return fmt.Sprintf("default code %q from %T used with status %d: use its own sentinel, or pick a code of your own", code, coded, res.status)
		}
		res.code = code
	}

	var ra interface{ RetryAfter() time.Duration }
	if errors.As(err, &ra) {
		d := ra.RetryAfter()
		if res.status != http.StatusTooManyRequests && res.status != http.StatusServiceUnavailable {
			return fmt.Sprintf("Retry-After from %T on status %d: it is only allowed on 429 (api.ErrTooManyRequests) and 503 (api.ErrUnavailable)", ra, res.status)
		}
		if d <= 0 {
			return fmt.Sprintf("Retry-After from %T with duration %s: it must be > 0", ra, d)
		}
		res.retryAfter = d
	}
	return ""
}

func isSentinel(err error) bool {
	for _, s := range sentinels {
		if err == s.err {
			return true
		}
	}
	return false
}

func isDefaultCode(code string) bool {
	if code == codeInternal {
		return true
	}
	for _, s := range sentinels {
		if s.code == code {
			return true
		}
	}
	return false
}

// retryAfterSeconds renders d as whole seconds, rounded up.
func retryAfterSeconds(d time.Duration) string {
	return strconv.FormatInt(int64((d+time.Second-1)/time.Second), 10)
}

// writeError writes the error response for err. Every error response of the
// API goes through it, except the statuses schemaf answers itself
// (writeStatusError). 5xx get an id under which the full error is logged.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	res := resolveError(err)
	body := errorResponse{Error: res.msg, Code: res.code}
	if res.status >= 500 {
		body.ID = newErrorID()
		args := []any{
			"id", body.ID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", res.status,
			"code", res.code,
			"error", err.Error(),
		}
		if sub, ok := Subject(r.Context()); ok {
			args = append(args, "subject", sub)
		}
		if res.misuse != "" {
			args = append(args, "misuse", res.misuse)
		}
		log.ErrorCtx(r.Context(), "api: server error", args...)
	}
	if res.retryAfter > 0 {
		w.Header().Set("Retry-After", retryAfterSeconds(res.retryAfter))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.status)
	json.NewEncoder(w).Encode(body)
}

// frameworkStatus is a status schemaf answers itself, never an endpoint, so
// there is no sentinel for it.
type frameworkStatus struct {
	status int
	code   string
}

var (
	statusMethodNotAllowed     = frameworkStatus{http.StatusMethodNotAllowed, "method_not_allowed"}         // route gate
	statusNotAcceptable        = frameworkStatus{http.StatusNotAcceptable, "not_acceptable"}                // Accept vs Produces()
	statusUnsupportedMediaType = frameworkStatus{http.StatusUnsupportedMediaType, "unsupported_media_type"} // Content-Type vs Consumes()
)

// frameworkStatuses lists every frameworkStatus, for the Error schema.
var frameworkStatuses = []frameworkStatus{statusMethodNotAllowed, statusNotAcceptable, statusUnsupportedMediaType}

// writeStatusError writes an error response for a status schemaf answers
// itself (see frameworkStatuses). Never logged, no id.
func writeStatusError(w http.ResponseWriter, s frameworkStatus, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.status)
	json.NewEncoder(w).Encode(errorResponse{Error: msg, Code: s.code})
}

// newErrorID returns 16 random hex characters.
func newErrorID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
