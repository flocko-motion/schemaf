// Package tool provides high-level utilities for common AI workflows.
// It simplifies single-shot "tool call" patterns where a consistent action
// is applied to varying inputs.
package tool

import (
	"context"
	"fmt"

	"schemaf.local/base/ai"
)

// Config holds configuration for a Tool instance.
// All fields are optional and will use sensible defaults if not specified.
type Config struct {
	// SystemPrompt defines the tool's behavior and role.
	// This is cached automatically when the tool is reused.
	SystemPrompt string

	// MaxTokens is the maximum number of tokens to generate.
	// Defaults to 4096 if not specified.
	MaxTokens int

	// Temperature controls randomness (0.0 = deterministic, 1.0 = creative).
	// Defaults to 0.7 if not specified.
	Temperature float64

	// CacheTTL is the cache time-to-live for the system prompt.
	// Valid values: "5m" (5 minutes) or "1h" (1 hour).
	// Defaults to "5m" if not specified.
	CacheTTL string
}

// Tool represents a reusable AI tool with consistent behavior.
// Create a Tool instance with New(), then call Run() multiple times
// with different inputs. The system prompt is cached for efficiency.
type Tool struct {
	client ai.Client
	config Config
}

// New creates a new Tool instance with the given configuration.
// The configuration is applied once and reused for all subsequent Run() calls.
func New(client ai.Client, config Config) *Tool {
	// Apply defaults for optional fields
	if config.MaxTokens == 0 {
		config.MaxTokens = 4096
	}
	if config.Temperature == 0 {
		config.Temperature = 0.7
	}
	if config.CacheTTL == "" {
		config.CacheTTL = "5m"
	}

	return &Tool{
		client: client,
		config: config,
	}
}

// Run executes the tool on the given input string.
// The input is wrapped in XML document tags to provide clear semantic separation
// from the system prompt. This is especially important for inputs that might
// contain confusing content (like conversation logs with user/assistant markers).
//
// The system prompt is cached automatically for efficiency when the tool is
// reused multiple times.
func (t *Tool) Run(ctx context.Context, input string) (string, error) {
	resp, err := t.RunWithResponse(ctx, input)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// RunWithResponse executes the tool and returns the full completion response.
// This includes token usage information for cost tracking.
func (t *Tool) RunWithResponse(ctx context.Context, input string) (*ai.CompletionResponse, error) {
	// Wrap input in XML document structure for semantic clarity
	userMessage := fmt.Sprintf(
		`<document><source>input.txt</source><document_content>
%s
</document_content></document>`,
		input,
	)

	// Build completion request with cached system prompt
	req := ai.CompletionRequest{
		SystemPrompt: t.config.SystemPrompt,
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: userMessage},
		},
		MaxTokens:   t.config.MaxTokens,
		Temperature: t.config.Temperature,
		CacheConfig: &ai.CacheConfig{
			CacheSystemPrompt: true,
			TTL:               t.config.CacheTTL,
		},
	}

	// Execute completion
	resp, err := t.client.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("completion failed: %w", err)
	}

	return resp, nil
}
