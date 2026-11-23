// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/flocko-motion/schemaf/constants"
)

var frontendFS fs.FS

// SetFrontend registers the embedded frontend filesystem for production serving.
// In development mode (see DevMode), the server proxies to the frontend dev server instead.
func SetFrontend(fsys fs.FS) {
	frontendFS = fsys
}

// frontendHandler returns the appropriate handler for non-API requests:
//   - proxy (development mode): reverse proxy to the frontend dev server
//   - otherwise: serve embedded static files with SPA fallback
func frontendHandler(proxy bool, version string) (http.Handler, error) {
	if proxy {
		return devProxy(), nil
	}
	return prodFileServer(version)
}

// devProxy returns a reverse proxy targeting the frontend dev server.
func devProxy() http.Handler {
	target, _ := url.Parse(fmt.Sprintf("http://localhost:%d", constants.FrontendPort()))
	return httputil.NewSingleHostReverseProxy(target)
}

// prodFileServer serves embedded frontend assets with SPA fallback.
// If a requested path matches a real file, it's served directly.
// Otherwise, index.html is served (for client-side routing). index.html, at /
// and on the fallback, carries the version's meta tag (indexWithVersion).
func prodFileServer(version string) (http.Handler, error) {
	if frontendFS == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no frontend configured", http.StatusNotFound)
		}), nil
	}

	index, hasIndex, err := indexWithVersion(frontendFS, version)
	if err != nil {
		return nil, err
	}
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	}
	fileServer := http.FileServer(http.FS(frontendFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try the requested path first.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			if hasIndex {
				serveIndex(w, r)
				return
			}
			path = "index.html"
		}

		if _, err := fs.Stat(frontendFS, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for unmatched paths.
		if hasIndex {
			serveIndex(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}), nil
}

// indexWithVersion returns the embedded index.html with
// <meta name="schemaf-version" content="<version>"> inserted before its
// first </head>, and whether there is an index.html at all. An index.html
// without </head> is an error. See EXTEND.md, "Noticing an upgrade in an
// open page".
func indexWithVersion(fsys fs.FS, version string) ([]byte, bool, error) {
	b, err := fs.ReadFile(fsys, "index.html")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("api: reading the embedded index.html: %w", err)
	}
	i := indexFold(b, []byte("</head>"))
	if i < 0 {
		return nil, false, errors.New("api: the embedded index.html has no </head>: schemaf inserts the version's meta tag before it (EXTEND.md, Noticing an upgrade in an open page) — add <head>…</head> to frontend/index.html")
	}
	meta := `<meta name="schemaf-version" content="` + html.EscapeString(version) + `">`
	out := make([]byte, 0, len(b)+len(meta))
	out = append(out, b[:i]...)
	out = append(out, meta...)
	out = append(out, b[i:]...)
	return out, true, nil
}

// indexFold returns the index of the first occurrence of the ASCII sep in s,
// ignoring ASCII case, or -1.
func indexFold(s, sep []byte) int {
	for i := 0; i+len(sep) <= len(s); i++ {
		if bytes.EqualFold(s[i:i+len(sep)], sep) {
			return i
		}
	}
	return -1
}
