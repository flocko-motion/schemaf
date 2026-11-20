// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Limits of the error details store. Not configurable.
const (
	errorDetailsMax = 1000           // newest entries kept
	errorDetailsTTL = 24 * time.Hour // maximum age of an entry
)

// ErrorDetails is what schemaf stores about a 5xx, under the id its
// response carries. Never the request body or headers. See EXTEND.md,
// "Error details".
type ErrorDetails struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Method  string    `json:"method"`
	Path    string    `json:"path"`
	Status  int       `json:"status"`
	Code    string    `json:"code"`
	Subject string    `json:"subject,omitempty"` // the caller, if authenticated
	Error   string    `json:"error"`             // the full err.Error()
	Misuse  string    `json:"misuse,omitempty"`  // the misuse of the error API, if any
}

// errorStore keeps the newest errorDetailsMax error details, each for at
// most errorDetailsTTL, in memory.
type errorStore struct {
	mu      sync.Mutex
	entries []ErrorDetails // oldest first
	byID    map[string]ErrorDetails
}

func newErrorStore() *errorStore {
	return &errorStore{byID: map[string]ErrorDetails{}}
}

// errorDetails is the store WriteError records into. Package-level, so a
// standalone server using WriteError outside the schemaf app records too.
var errorDetails = newErrorStore()

// record stores d and evicts what is over the limits.
func (s *errorStore) record(d ErrorDetails) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[d.ID] = d
	s.entries = append(s.entries, d)
	s.evict()
}

// evict drops entries over the count limit and entries older than the TTL,
// oldest first. Callers hold s.mu.
func (s *errorStore) evict() {
	cutoff := now().Add(-errorDetailsTTL)
	n := 0
	for n < len(s.entries) && (len(s.entries)-n > errorDetailsMax || !s.entries[n].Time.After(cutoff)) {
		delete(s.byID, s.entries[n].ID)
		n++
	}
	// append copies only the kept entries once the array is full, so
	// slicing off the front keeps memory bounded.
	s.entries = s.entries[n:]
}

// lookup returns the entry stored under id, if it is still kept.
func (s *errorStore) lookup(id string) (ErrorDetails, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evict()
	d, ok := s.byID[id]
	return d, ok
}

// len returns the number of entries kept.
func (s *errorStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evict()
	return len(s.entries)
}

// LookupError returns the details of the 5xx answered with id, if they are
// still stored: the newest 1000, each for at most 24 hours, in this process
// only (lost on restart, per replica). A standalone server using WriteError
// can serve its own lookup with it.
func LookupError(id string) (ErrorDetails, bool) {
	return errorDetails.lookup(id)
}

// CanSeeErrorDetailsFunc decides whether subject may look up error details
// in production.
type CanSeeErrorDetailsFunc func(ctx context.Context, subject string) bool

var canSeeErrorDetails CanSeeErrorDetailsFunc

// SetCanSeeErrorDetails sets who may look up error details in production.
// nil means nobody. Called by the schemaf app at server start (see
// app.CanSeeErrorDetails).
func SetCanSeeErrorDetails(fn CanSeeErrorDetailsFunc) {
	canSeeErrorDetails = fn
}

// errorDetailsHandler serves GET /api/errors/{id} (AuthOptional): open in
// development mode; in production only to subjects the predicate allows.
func errorDetailsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !DevMode() {
			sub, ok := Subject(r.Context())
			if !ok {
				writeAuthError(w, r, unauthorized("error details need a signed-in caller"))
				return
			}
			if canSeeErrorDetails == nil || !canSeeErrorDetails(r.Context(), sub) {
				WriteError(w, r, Error(ErrForbidden, "forbidden", "not allowed to see error details"))
				return
			}
		}
		d, ok := LookupError(r.PathValue("id"))
		if !ok {
			WriteError(w, r, Error(ErrNotFound, "not_found", "no error details for this id: unknown, or expired"))
			return
		}
		writeJSON(w, d)
	})
}
