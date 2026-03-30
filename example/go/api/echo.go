package api

import (
	"io"
	"net/http"
)

// EchoRawEndpoint echoes the request body back as-is.
// Demonstrates HandleRaw for non-JSON binary endpoints.
type EchoRawEndpoint struct{}

func (e EchoRawEndpoint) Method() string { return "POST" }
func (e EchoRawEndpoint) Path() string   { return "/api/echo" }
func (e EchoRawEndpoint) Auth() bool     { return false }

func (e EchoRawEndpoint) HandleRaw(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
	_, err := io.Copy(w, r.Body)
	return err
}
