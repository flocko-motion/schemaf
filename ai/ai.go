// Package ai provides a unified interface for working with multiple LLM providers.
// It offers a simple, opinionated abstraction layer designed for tool-building workflows.
package ai

import "context"

// Client represents an LLM provider client.
// Implementations include Anthropic Claude, OpenAI, and others.
type Client interface {
	// Complete sends a completion request and returns the response.
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)

	// Provider returns the provider name (e.g., "anthropic", "openai").
	Provider() string
}

// CompletionRequest is a provider-agnostic completion request.
type CompletionRequest struct {
	// SystemPrompt defines the AI's role and behavior.
	// This is typically cached when the same prompt is reused.
	SystemPrompt string

	// Messages is the conversation history.
	Messages []Message

	// MaxTokens is the maximum number of tokens to generate.
	MaxTokens int

	// Temperature controls randomness (0.0 = deterministic, 1.0 = creative).
	Temperature float64

	// Model is an optional model override.
	// If empty, the client's default model is used.
	Model string

	// CacheConfig controls prompt caching behavior.
	CacheConfig *CacheConfig
}

// CompletionResponse is a provider-agnostic completion response.
type CompletionResponse struct {
	// Content is the generated text response.
	Content string

	// StopReason indicates why generation stopped (e.g., "end_turn", "max_tokens").
	StopReason string

	// InputTokens is the number of input tokens processed.
	InputTokens int

	// OutputTokens is the number of output tokens generated.
	OutputTokens int

	// CacheReadTokens is the number of tokens read from cache (if caching enabled).
	CacheReadTokens int

	// CacheCreationTokens is the number of tokens written to cache (if caching enabled).
	CacheCreationTokens int
}

// Message represents a single message in a conversation.
type Message struct {
	// Role is the message role (user or assistant).
	Role MessageRole

	// Content is the message text.
	Content string
}

// MessageRole identifies the speaker of a message.
type MessageRole string

const (
	// RoleUser represents a user message.
	RoleUser MessageRole = "user"

	// RoleAssistant represents an assistant message.
	RoleAssistant MessageRole = "assistant"
)

// CacheConfig controls prompt caching behavior.
type CacheConfig struct {
	// CacheSystemPrompt enables caching of the system prompt.
	// This is beneficial when reusing the same system prompt across multiple requests.
	CacheSystemPrompt bool

	// TTL is the cache time-to-live ("5m" or "1h").
	// Defaults to "5m" (5 minutes).
	TTL string
}
