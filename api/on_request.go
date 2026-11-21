// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
)

// OnRequestFunc is the per-request hook: it runs on every API call after
// authentication and before the handler. a is the request's admission (see
// Admission). The returned context is the one the handler gets; a returned
// error answers the request like an endpoint error (see WriteError) and the
// handler does not run. See EXTEND.md, "Per-request hook".
type OnRequestFunc func(ctx context.Context, a *Admission) (context.Context, error)

// Admission is the authenticated caller of one request, handed only to the
// OnRequest hook and valid only while it runs. Only schemaf issues a valid
// one: a zero value, a nil pointer, or an admission whose hook has returned
// (and every copy of it) is invalid. An app makes its own caller type
// unforgeable by requiring an *Admission in its only constructor. See
// EXTEND.md, "The admission".
type Admission struct {
	subject string
	ok      bool
	// live is shared by every copy, so invalidating it invalidates them all.
	live *atomic.Bool
}

// Valid reports whether a was issued by schemaf and its OnRequest hook is
// still running.
func (a *Admission) Valid() bool {
	return a != nil && a.live != nil && a.live.Load()
}

// Subject returns the authenticated subject, or ("", false) for an anonymous
// caller and on every AuthNone route. It panics if a is not valid (see
// Valid), never reporting an anonymous caller instead.
func (a *Admission) Subject() (string, bool) {
	switch {
	case a == nil:
		panic("api: Admission.Subject called on a nil admission; use the one schemaf passes to the OnRequest hook")
	case a.live == nil:
		panic("api: Admission.Subject called on an admission not issued by schemaf; use the one schemaf passes to the OnRequest hook")
	case !a.live.Load():
		panic("api: Admission.Subject called after its OnRequest hook returned; an admission is valid only while its hook runs")
	}
	return a.subject, a.ok
}

var onRequest OnRequestFunc

// SetOnRequest sets the per-request hook. nil means none. Called by the
// schemaf app at server start.
func SetOnRequest(fn OnRequestFunc) {
	onRequest = fn
}

// runOnRequest runs the OnRequest hook (if any) for r, whose context already
// carries the authentication result, then next with the hook's context.
func runOnRequest(w http.ResponseWriter, r *http.Request, next http.Handler) {
	hook := onRequest
	if hook == nil {
		next.ServeHTTP(w, r)
		return
	}
	ctx, err := admit(hook, r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if ctx == nil {
		WriteError(w, r, errors.New("api: the OnRequest hook returned a nil context and a nil error; return the context the handler should get"))
		return
	}
	next.ServeHTTP(w, r.WithContext(ctx))
}

// admit calls hook with a fresh admission for the caller in ctx and
// invalidates it when the hook returns, also if it panics.
func admit(hook OnRequestFunc, ctx context.Context) (context.Context, error) {
	subject, ok := Subject(ctx)
	live := new(atomic.Bool)
	live.Store(true)
	defer live.Store(false)
	return hook(ctx, &Admission{subject: subject, ok: ok, live: live})
}
