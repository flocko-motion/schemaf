// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/flocko-motion/schemaf/constants"
)

var frontendFS fs.FS

// SetFrontend registers the embedded frontend filesystem for production serving.
// In dev mode (SCHEMAF_ENV != "docker"), the server proxies to the frontend dev server instead.
func SetFrontend(fsys fs.FS) {
	frontendFS = fsys
}

// frontendHandler returns the appropriate handler for non-API requests:
//   - Dev: reverse proxy to the frontend dev server
//   - Prod: serve embedded static files with SPA fallback
func frontendHandler() http.Handler {
	if os.Getenv("SCHEMAF_ENV") != "docker" {
		return devProxy()
	}
	return prodFileServer()
}

// devProxy returns a reverse proxy targeting the frontend dev server.
func devProxy() http.Handler {
	target, _ := url.Parse(fmt.Sprintf("http://localhost:%d", constants.FrontendPort()))
	return httputil.NewSingleHostReverseProxy(target)
}

// prodFileServer serves embedded frontend assets with SPA fallback.
// If a requested path matches a real file, it's served directly.
// Otherwise, index.html is served (for client-side routing).
func prodFileServer() http.Handler {
	if frontendFS == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no frontend configured", http.StatusNotFound)
		})
	}

	fileServer := http.FileServer(http.FS(frontendFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try the requested path first.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(frontendFS, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for unmatched paths.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
