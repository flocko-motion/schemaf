// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// NewHTTPClient creates a new HTTP client with default settings
func NewHTTPClient(verbose bool) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		headers: make(map[string]string),
		verbose: verbose,
	}
}

// WithTimeout sets the HTTP client timeout in seconds
func (c *HTTPClient) WithTimeout(seconds int) *HTTPClient {
	c.client.Timeout = time.Duration(seconds) * time.Second
	return c
}

// WithHeader sets a header for all requests
func (c *HTTPClient) WithHeader(key, value string) *HTTPClient {
	c.headers[key] = value
	return c
}

// Get performs a GET request
func (c *HTTPClient) Get(url string) (*Response, error) {
	return c.do("GET", url, nil)
}

// Post performs a POST request with JSON body
func (c *HTTPClient) Post(url string, body interface{}) (*Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	return c.do("POST", url, bodyReader)
}

// do performs the HTTP request
func (c *HTTPClient) do(method, url string, body io.Reader) (*Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	// Log request if verbose
	if c.verbose {
		Print("→ %s %s\n", method, url)
	}

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log response if verbose
	if c.verbose {
		Print("← %d (%d bytes)\n", resp.StatusCode, len(respBody))
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Headers:    resp.Header,
	}, nil
}

// JSON unmarshals the response body into v
func (r *Response) JSON(v interface{}) error {
	return json.Unmarshal(r.Body, v)
}

// String returns the response body as a string
func (r *Response) String() string {
	return string(r.Body)
}

// IsSuccess returns true if status code is 2xx
func (r *Response) IsSuccess() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}
