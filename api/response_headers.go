// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
)

// VersionHeader carries the running version (RunningVersion) on every
// response. See EXTEND.md, "Noticing an upgrade in an open page".
const VersionHeader = "X-Schemaf-Version"

// reservedHeaderPrefix is the header prefix schemaf owns.
const reservedHeaderPrefix = "X-Schemaf-"

// nativeDevTag is the tag of the placeholder image the dev command sets as
// SCHEMAF_IMAGE (schemaf/dev.go, nativeDevImage; a test there ties the two):
// the backend runs natively, no image runs.
const nativeDevTag = "dev-runs-natively"

// RunningVersion returns the tag of the running backend image, as it is (a
// release such as 1.5.0, a commit such as 3f9a2c1-dirty, or a --tag), or
// "dev" when no image runs (development, integration tests). Every response
// carries it in X-Schemaf-Version. See EXTEND.md, "Noticing an upgrade in an
// open page".
func RunningVersion() string {
	img := os.Getenv("SCHEMAF_IMAGE")
	if img == "" {
		return "dev"
	}
	tag := img[strings.LastIndex(img, ":")+1:]
	if tag == "" || tag == nativeDevTag {
		return "dev"
	}
	return tag
}

// ResponseHeadersFunc is the response-headers hook: it runs on every response
// before routing and authentication, and adds headers to h. See EXTEND.md,
// "Response headers".
type ResponseHeadersFunc func(r *http.Request, h http.Header)

var onResponseHeaders ResponseHeadersFunc

// SetOnResponseHeaders sets the response-headers hook. nil means none.
// Called by the schemaf app at server start.
func SetOnResponseHeaders(fn ResponseHeadersFunc) {
	onResponseHeaders = fn
}

// withResponseHeaders wraps next: every response gets schemaf's own headers
// (X-Schemaf-Version: version), then the hook's. A hook that sets, changes
// or deletes an X-Schemaf-* header fails the request with a 500 instead of
// reaching next.
func withResponseHeaders(version string, hook ResponseHeadersFunc, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set(VersionHeader, version)
		if hook != nil {
			hook(r, h)
			if bad := reservedHeaderChanged(h, version); bad != "" {
				for k := range h {
					if hasReservedPrefix(k) {
						delete(h, k)
					}
				}
				h.Set(VersionHeader, version)
				WriteError(w, r, fmt.Errorf("api: the OnResponseHeaders hook set %s: the %s prefix is reserved for schemaf", bad, reservedHeaderPrefix))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// reservedHeaderChanged returns the X-Schemaf-* header the hook touched, or
// "" if schemaf's headers are exactly as schemaf set them.
func reservedHeaderChanged(h http.Header, version string) string {
	var extra []string
	for k := range h {
		if hasReservedPrefix(k) && k != VersionHeader {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return extra[0]
	}
	if v := h[VersionHeader]; len(v) != 1 || v[0] != version {
		return VersionHeader
	}
	return ""
}

// hasReservedPrefix reports whether header name k starts with X-Schemaf-, in
// any case (a hook may write the map directly, bypassing canonicalisation).
func hasReservedPrefix(k string) bool {
	return len(k) >= len(reservedHeaderPrefix) && strings.EqualFold(k[:len(reservedHeaderPrefix)], reservedHeaderPrefix)
}
