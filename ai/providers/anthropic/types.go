// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package anthropic

// messagesRequest represents the Anthropic Messages API request format.
type messagesRequest struct {
	Model       string         `json:"model"`
	MaxTokens   int            `json:"max_tokens"`
	Temperature float64        `json:"temperature,omitempty"`
	System      []systemBlock  `json:"system,omitempty"`
	Messages    []messageBlock `json:"messages"`
}

// systemBlock represents a system prompt block with optional caching.
type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

// messageBlock represents a message in the conversation.
type messageBlock struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// cacheControl specifies caching behavior for a content block.
type cacheControl struct {
	Type string `json:"type"`          // "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "5m" or "1h"
}

// messagesResponse represents the Anthropic Messages API response format.
type messagesResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      usage          `json:"usage"`
}

// contentBlock represents a piece of content in the response.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// usage contains token usage statistics.
type usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// errorResponse represents an error response from the API.
type errorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}
