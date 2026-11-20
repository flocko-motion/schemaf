// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// NewRoute creates a Route from a typed Endpoint, wiring up request decoding,
// response encoding (by Consumes()/Produces(), JSON by default) and auth.
// summary and description are extracted from the struct's doc comment by
// codegen and passed as string literals in the generated Provider().
func NewRoute[Req, Resp any](e Endpoint[Req, Resp], summary, description string) Route {
	consumes, produces := mediaTypes(e, "Consumes"), mediaTypes(e, "Produces")
	return Route{
		Method:      e.Method(),
		Path:        e.Path(),
		Auth:        e.Auth(),
		Summary:     summary,
		Description: description,
		Handler:     withAuth(e.Auth(), newTypedHandler(e, consumes, produces)),
	}
}

// hasBody reports whether schemaf decodes a request body for method.
func hasBody(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
}

// decodeRequest fills req from path params, query params and, for methods
// that carry one, the body (by Content-Type). It writes the error response
// and returns false on failure.
func decodeRequest(w http.ResponseWriter, r *http.Request, req any, consumes []string) bool {
	if err := decodePathParams(r, req); err != nil {
		WriteError(w, r, Error(ErrBadRequest, "bad_request", err.Error()))
		return false
	}
	if err := decodeQueryParams(r, req); err != nil {
		WriteError(w, r, Error(ErrBadRequest, "bad_request", err.Error()))
		return false
	}
	if !hasBody(r.Method) {
		return true
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, r, Error(ErrBadRequest, "bad_request", "reading request body: "+err.Error()))
		return false
	}
	if len(body) == 0 {
		return true
	}
	c, err := requestCodec(r.Header.Get("Content-Type"), consumes)
	if err != nil {
		writeStatusError(w, statusUnsupportedMediaType,
			fmt.Sprintf("Content-Type %q is not supported; send one of: %s", r.Header.Get("Content-Type"), strings.Join(consumes, ", ")))
		return false
	}
	if err := c.decode(body, req); err != nil {
		WriteError(w, r, Error(ErrBadRequest, "bad_request", "invalid request body: "+err.Error()))
		return false
	}
	return true
}

func newTypedHandler[Req, Resp any](e Endpoint[Req, Resp], consumes, produces []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Negotiate first: a request nothing can answer must not run Handle.
		mt, ok := negotiate(r.Header.Get("Accept"), produces)
		if !ok {
			writeStatusError(w, statusNotAcceptable,
				fmt.Sprintf("Accept %q matches none of: %s", r.Header.Get("Accept"), strings.Join(produces, ", ")))
			return
		}
		var req Req
		if !decodeRequest(w, r, &req, consumes) {
			return
		}

		ctx, er := withEndpointRequest(w, r)
		resp, err := e.Handle(ctx, req)
		if err != nil {
			er.commit(func() { WriteError(w, r, err) })
			return
		}

		// Encode into a buffer, so an encoding error can still answer 500.
		var buf bytes.Buffer
		if err := codecs[mt].encode(&buf, resp); err != nil {
			er.commit(func() { WriteError(w, r, fmt.Errorf("encoding response as %s: %w", mt, err)) })
			return
		}
		er.commit(func() {
			if len(produces) > 1 {
				w.Header().Set("Vary", "Accept")
			}
			w.Header().Set("Content-Type", mt)
			w.Write(buf.Bytes())
		})
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
// the framework writes the standard error response (see WriteError).
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
	}
}

func newRawHandler(e RawEndpoint) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := e.HandleRaw(w, r); err != nil {
			WriteError(w, r, err)
		}
	})
}
