package anthropic

import (
	"encoding/json"
	"fmt"

	"atlas.local/base/ai"
)

// buildRequest converts a generic CompletionRequest to an Anthropic-specific request.
func buildRequest(req ai.CompletionRequest, model string) messagesRequest {
	apiReq := messagesRequest{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Messages:    convertMessages(req.Messages),
	}

	// Add system prompt with cache control if present
	if req.SystemPrompt != "" && req.CacheConfig != nil && req.CacheConfig.CacheSystemPrompt {
		ttl := req.CacheConfig.TTL
		if ttl == "" {
			ttl = "5m"
		}

		apiReq.System = []systemBlock{
			{
				Type: "text",
				Text: req.SystemPrompt,
				CacheControl: &cacheControl{
					Type: "ephemeral",
					TTL:  ttl,
				},
			},
		}
	} else if req.SystemPrompt != "" {
		// System prompt without caching
		apiReq.System = []systemBlock{
			{
				Type: "text",
				Text: req.SystemPrompt,
			},
		}
	}

	return apiReq
}

// convertMessages converts generic Messages to Anthropic message blocks.
func convertMessages(messages []ai.Message) []messageBlock {
	blocks := make([]messageBlock, len(messages))
	for i, msg := range messages {
		blocks[i] = messageBlock{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}
	return blocks
}

// parseResponse converts an Anthropic response to a generic CompletionResponse.
func parseResponse(apiResp *messagesResponse) *ai.CompletionResponse {
	// Extract text from content blocks
	var text string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	return &ai.CompletionResponse{
		Content:             text,
		StopReason:          apiResp.StopReason,
		InputTokens:         apiResp.Usage.InputTokens,
		OutputTokens:        apiResp.Usage.OutputTokens,
		CacheReadTokens:     apiResp.Usage.CacheReadInputTokens,
		CacheCreationTokens: apiResp.Usage.CacheCreationInputTokens,
	}
}

// handleErrorResponse parses an error response and returns an appropriate Error.
func handleErrorResponse(status int, body []byte) error {
	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return ai.NewServerError(fmt.Sprintf("HTTP %d: failed to parse error response", status), status)
	}

	message := errResp.Error.Message
	if message == "" {
		message = fmt.Sprintf("HTTP %d", status)
	}

	switch status {
	case 401:
		return ai.NewAuthError(message)
	case 429:
		return ai.NewRateLimitError(message)
	case 400:
		return ai.NewInvalidRequestError(message)
	default:
		return ai.NewServerError(message, status)
	}
}
