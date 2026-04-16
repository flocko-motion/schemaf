// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Endpoint is the interface that all schemaf API endpoints must implement.
// Req is the request type (decoded from JSON body or path/query params).
// Resp is the response type (encoded as JSON).
//
// Document your endpoint with a Go doc comment on the struct:
//
//	// ListTodosEndpoint returns all todo items ordered by creation date.
//	// Returns an empty array if no todos exist.
//	type ListTodosEndpoint struct{}
//
// The first line becomes the OpenAPI summary; subsequent lines the description.
// Both are extracted by codegen — no annotations required.
type Endpoint[Req, Resp any] interface {
	Method() string // HTTP method: "GET", "POST", "PUT", "DELETE", etc.
	Path() string   // Route path, e.g. "/api/todos/{id}"
	Auth() bool     // Whether the endpoint requires JWT authentication
	Handle(ctx context.Context, req Req) (Resp, error)
}

// NewRoute creates a Route from a typed Endpoint, wiring up JSON decode/encode
// and auth declaration. summary and description are extracted from the struct's
// doc comment by codegen and passed as string literals in the generated Provider().
func NewRoute[Req, Resp any](e Endpoint[Req, Resp], summary, description string) Route {
	var zeroReq Req
	var zeroResp Resp
	h := newTypedHandler(e)
	if e.Auth() {
		h = requireAuth(h)
	}
	return Route{
		Method:      e.Method(),
		Path:        e.Path(),
		Auth:        e.Auth(),
		Summary:     summary,
		Description: description,
		Handler:     h,
		ReqType:     &zeroReq,
		RespType:    &zeroResp,
	}
}

func newTypedHandler[Req, Resp any](e Endpoint[Req, Resp]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Req
		// Decode path params (fields tagged with `path:"name"`).
		if err := decodePathParams(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Decode query params (fields tagged with `query:"name"`).
		if err := decodeQueryParams(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Decode body for methods that carry one.
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
				writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
		}

		resp, err := e.Handle(r.Context(), req)
		if err != nil {
			writeJSONError(w, statusFor(err), err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "encoding response: "+err.Error())
		}
	})
}

// RawEndpoint is an alternative to Endpoint for handlers that need direct
// HTTP access — binary uploads, streaming downloads, SSE, etc.
//
// Define HandleRaw instead of Handle on your endpoint struct. Exactly one
// of Handle or HandleRaw must be present; codegen will error if both exist.
//
// The framework does not decode the request or encode the response — you
// have full control over http.ResponseWriter and *http.Request.
// If HandleRaw returns a non-nil error and no response has been written yet,
// the framework writes a JSON error response using the standard error mapping.
type RawEndpoint interface {
	Method() string
	Path() string
	Auth() bool
	HandleRaw(w http.ResponseWriter, r *http.Request) error
}

// NewRawRoute creates a Route from a RawEndpoint. No JSON decode/encode is
// performed; the handler receives the raw http.ResponseWriter and *http.Request.
func NewRawRoute(e RawEndpoint, summary, description string) Route {
	h := newRawHandler(e)
	if e.Auth() {
		h = requireAuth(h)
	}
	return Route{
		Method:      e.Method(),
		Path:        e.Path(),
		Auth:        e.Auth(),
		Summary:     summary,
		Description: description,
		Handler:     h,
		ReqType:     nil,
		RespType:    nil,
	}
}

func newRawHandler(e RawEndpoint) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := e.HandleRaw(w, r); err != nil {
			writeJSONError(w, statusFor(err), err.Error())
		}
	})
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
