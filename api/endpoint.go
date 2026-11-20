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

// AuthLevel declares how an endpoint authenticates its caller.
type AuthLevel int

const (
	// AuthNone: credentials are never looked at; the handler runs anonymous.
	AuthNone AuthLevel = iota
	// AuthOptional: the subject if credentials are present, anonymous if
	// not. Present but invalid credentials are a 401.
	AuthOptional
	// AuthRequired: 401 without valid credentials.
	AuthRequired
)

// String returns the constant's name, e.g. "AuthRequired".
func (l AuthLevel) String() string {
	switch l {
	case AuthNone:
		return "AuthNone"
	case AuthOptional:
		return "AuthOptional"
	case AuthRequired:
		return "AuthRequired"
	}
	return fmt.Sprintf("AuthLevel(%d)", int(l))
}

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
	Method() string  // HTTP method: "GET", "POST", "PUT", "DELETE", etc.
	Path() string    // Route path, e.g. "/api/todos/{id}"
	Auth() AuthLevel // AuthNone, AuthOptional or AuthRequired
	Handle(ctx context.Context, req Req) (Resp, error)
}

// NewRoute creates a Route from a typed Endpoint, wiring up JSON decode/encode
// and auth declaration. summary and description are extracted from the struct's
// doc comment by codegen and passed as string literals in the generated Provider().
func NewRoute[Req, Resp any](e Endpoint[Req, Resp], summary, description string) Route {
	var zeroReq Req
	var zeroResp Resp
	return Route{
		Method:      e.Method(),
		Path:        e.Path(),
		Auth:        e.Auth(),
		Summary:     summary,
		Description: description,
		Handler:     withAuth(e.Auth(), newTypedHandler(e)),
		ReqType:     &zeroReq,
		RespType:    &zeroResp,
	}
}

func newTypedHandler[Req, Resp any](e Endpoint[Req, Resp]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Req
		// Decode path params (fields tagged with `path:"name"`).
		if err := decodePathParams(r, &req); err != nil {
			writeError(w, r, Error(ErrBadRequest, "bad_request", err.Error()))
			return
		}
		// Decode query params (fields tagged with `query:"name"`).
		if err := decodeQueryParams(r, &req); err != nil {
			writeError(w, r, Error(ErrBadRequest, "bad_request", err.Error()))
			return
		}
		// Decode body for methods that carry one.
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
				writeError(w, r, Error(ErrBadRequest, "bad_request", "invalid request body: "+err.Error()))
				return
			}
		}

		resp, err := e.Handle(r.Context(), req)
		if err != nil {
			writeError(w, r, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			writeError(w, r, fmt.Errorf("encoding response: %w", err))
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
// the framework writes the standard error response (see writeError).
type RawEndpoint interface {
	Method() string
	Path() string
	Auth() AuthLevel
	HandleRaw(w http.ResponseWriter, r *http.Request) error
}

// NewRawRoute creates a Route from a RawEndpoint. No JSON decode/encode is
// performed; the handler receives the raw http.ResponseWriter and *http.Request.
func NewRawRoute(e RawEndpoint, summary, description string) Route {
	return Route{
		Method:      e.Method(),
		Path:        e.Path(),
		Auth:        e.Auth(),
		Summary:     summary,
		Description: description,
		Handler:     withAuth(e.Auth(), newRawHandler(e)),
		ReqType:     nil,
		RespType:    nil,
	}
}

func newRawHandler(e RawEndpoint) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := e.HandleRaw(w, r); err != nil {
			writeError(w, r, err)
		}
	})
}
