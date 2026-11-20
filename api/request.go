// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
)

// ctxKeyRequest is the context key for the endpoint request (*endpointRequest)
// that Handle and Stream run in.
type ctxKeyRequest struct{}

// endpointRequest is what a typed handler's ctx knows about its HTTP request.
type endpointRequest struct {
	r *http.Request

	mu        sync.Mutex
	w         http.ResponseWriter // the response the endpoint writes (for cookies)
	committed bool                // headers sent: no more cookies
}

var errHeadersSent = errors.New("auth: StartSessionCtx called after the response headers were sent: in a stream endpoint, start the session before Stream returns")

// setCookieHeader runs set (which sets headers on w) unless the headers were
// already sent.
func (er *endpointRequest) setCookieHeader(set func(w http.ResponseWriter) error) error {
	er.mu.Lock()
	defer er.mu.Unlock()
	if er.committed {
		return errHeadersSent
	}
	return set(er.w)
}

// commit runs writeHeaders (which sends the headers) and marks them sent, so
// a later StartSessionCtx fails instead of losing its cookie.
func (er *endpointRequest) commit(writeHeaders func()) {
	er.mu.Lock()
	defer er.mu.Unlock()
	er.committed = true
	writeHeaders()
}

func withEndpointRequest(w http.ResponseWriter, r *http.Request) (context.Context, *endpointRequest) {
	er := &endpointRequest{r: r, w: w}
	return context.WithValue(r.Context(), ctxKeyRequest{}, er), er
}

func endpointRequestFrom(ctx context.Context) *endpointRequest {
	er, _ := ctx.Value(ctxKeyRequest{}).(*endpointRequest)
	return er
}

// Header returns the first value of the request header name (case-insensitive,
// as http.Header.Get) in Handle and Stream. It is "" when the header is absent
// or ctx doesn't come from an endpoint request.
func Header(ctx context.Context, name string) string {
	er := endpointRequestFrom(ctx)
	if er == nil {
		return ""
	}
	return er.r.Header.Get(name)
}
