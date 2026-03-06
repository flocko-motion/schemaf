package api

import (
	"context"
	"encoding/json"
	"fmt"
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
		// Decode body for methods that carry one
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
		}

		resp, err := e.Handle(r.Context(), req)
		if err != nil {
			// TODO: map error types to status codes
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "encoding response: "+err.Error())
		}
	})
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
