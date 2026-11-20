// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flocko-motion/schemaf/log"
)

// MediaEventStream is the media type of a stream endpoint's response.
const MediaEventStream = "text/event-stream"

// streamKeepAlive is how long a stream stays silent before schemaf writes a
// keep-alive comment. A framework constant; a var only so tests can shorten it.
var streamKeepAlive = 15 * time.Second

// StreamEndpoint is a typed endpoint answering with server-sent events: each
// value sent on the returned channel is written as one event, its data the
// value as JSON. Closing the channel ends the stream; ctx is cancelled when
// the client disconnects, and the sending goroutine must then return.
// See EXTEND.md, "Stream Endpoints".
type StreamEndpoint[Req, Event any] interface {
	Method() string // always "GET"
	Path() string
	Auth() AuthLevel
	Stream(ctx context.Context, req Req) (<-chan Event, error)
}

// NewStreamRoute creates a Route from a StreamEndpoint. summary and
// description come from the struct's doc comment, as for NewRoute.
func NewStreamRoute[Req, Event any](e StreamEndpoint[Req, Event], summary, description string) Route {
	if e.Method() != http.MethodGet {
		panic(fmt.Sprintf("api: stream endpoint %T: method %q, must be GET", e, e.Method()))
	}
	return Route{
		Method:      e.Method(),
		Path:        e.Path(),
		Auth:        e.Auth(),
		Summary:     summary,
		Description: description,
		Handler:     withAuth(e.Auth(), newStreamHandler(e)),
	}
}

func newStreamHandler[Req, Event any](e StreamEndpoint[Req, Event]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Req
		if !decodeRequest(w, r, &req, nil) {
			return
		}
		ctx, er := withEndpointRequest(w, r)
		events, err := e.Stream(ctx, req)
		if err != nil {
			er.commit(func() { writeError(w, r, err) })
			return
		}
		if events == nil {
			er.commit(func() {
				writeError(w, r, fmt.Errorf("stream endpoint %T returned a nil channel and a nil error", e))
			})
			return
		}

		rc := http.NewResponseController(w)
		var flushErr error
		er.commit(func() {
			h := w.Header()
			h.Set("Content-Type", MediaEventStream)
			h.Set("Cache-Control", "no-cache")
			h.Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
			flushErr = rc.Flush()
		})
		if flushErr != nil {
			logStreamEnd(r, e, fmt.Errorf("flushing: %w", flushErr))
			return
		}

		keepAlive := time.NewTimer(streamKeepAlive)
		defer keepAlive.Stop()
		for {
			var chunk []byte
			select {
			case <-r.Context().Done(): // the client left
				return
			case <-keepAlive.C:
				chunk = []byte(": keepalive\n\n")
			case ev, ok := <-events:
				if !ok {
					return
				}
				data, err := json.Marshal(ev)
				if err != nil {
					logStreamEnd(r, e, fmt.Errorf("encoding event: %w", err))
					return
				}
				chunk = append(append([]byte("data: "), data...), '\n', '\n')
			}
			if _, err := w.Write(chunk); err != nil {
				return // the client left
			}
			if err := rc.Flush(); err != nil {
				return
			}
			keepAlive.Reset(streamKeepAlive)
		}
	})
}

// logStreamEnd logs why schemaf ended a stream it had already started.
func logStreamEnd(r *http.Request, e any, err error) {
	args := []any{"method", r.Method, "path", r.URL.Path, "endpoint", fmt.Sprintf("%T", e), "error", err.Error()}
	if sub, ok := Subject(r.Context()); ok {
		args = append(args, "subject", sub)
	}
	log.ErrorCtx(r.Context(), "api: stream ended", args...)
}
