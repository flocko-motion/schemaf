// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/flocko-motion/schemaf/ai"
)

// Client is an Anthropic API client.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// Option is a functional option for configuring the Client.
type Option func(*Client)

// WithModel sets the model to use.
func WithModel(model string) Option {
	return func(c *Client) {
		c.model = model
	}
}

// WithTimeout sets the HTTP timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithBaseURL sets the API base URL (primarily for testing).
func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = url
	}
}

// NewAnthropic creates a new Anthropic client.
// The API key is loaded automatically from ~/.schemaf/.env.
func NewAnthropic(opts ...Option) (*Client, error) {
	apiKey, err := ai.GetAPIKey("anthropic")
	if err != nil {
		return nil, ai.NewAuthError(fmt.Sprintf("failed to load API key: %v", err))
	}

	client := &Client{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com/v1",
		model:   GetModel(ModelFastCheap), // Default to fast+cheap
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

// Provider returns the provider name.
func (c *Client) Provider() string {
	return "anthropic"
}

// Complete sends a completion request to the Anthropic API.
func (c *Client) Complete(ctx context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	// Use provided model or default
	model := req.Model
	if model == "" {
		model = c.model
	}

	// Check if system prompt is long enough for caching
	if req.CacheConfig != nil && req.CacheConfig.CacheSystemPrompt {
		systemTokens := estimateTokens(req.SystemPrompt)
		minTokens := GetMinCacheTokens(model)
		if systemTokens < minTokens {
			// Disable caching if below minimum
			req.CacheConfig.CacheSystemPrompt = false
		}
	}

	// Build Anthropic-specific request
	apiReq := buildRequest(req, model)

	// Marshal request body
	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, ai.NewInvalidRequestError(fmt.Sprintf("failed to marshal request: %v", err))
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, ai.NewNetworkError("failed to create request", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// Send request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, ai.NewNetworkError("HTTP request failed", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, ai.NewNetworkError("failed to read response", err)
	}

	// Handle error responses
	if httpResp.StatusCode != 200 {
		return nil, handleErrorResponse(httpResp.StatusCode, respBody)
	}

	// Parse success response
	var apiResp messagesResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, ai.NewServerError(fmt.Sprintf("failed to parse response: %v", err), httpResp.StatusCode)
	}

	return parseResponse(&apiResp), nil
}

// estimateTokens provides a rough estimate of token count.
// Rough heuristic: ~4 characters per token.
func estimateTokens(text string) int {
	return len(text) / 4
}
