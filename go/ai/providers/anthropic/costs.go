package anthropic

// Model pricing in USD per million tokens (as of 2025)
// https://www.anthropic.com/pricing

const (
	// Opus 4.6 pricing (per MTok)
	OpusInputPrice      = 5.00
	OpusOutputPrice     = 25.00
	OpusCacheWritePrice = 6.25 // 1.25x base
	OpusCacheReadPrice  = 0.50 // 0.1x base

	// Sonnet 4.6 pricing (per MTok)
	SonnetInputPrice      = 3.00
	SonnetOutputPrice     = 15.00
	SonnetCacheWritePrice = 3.75 // 1.25x base
	SonnetCacheReadPrice  = 0.30 // 0.1x base

	// Haiku 4.5 pricing (per MTok)
	HaikuInputPrice      = 1.00
	HaikuOutputPrice     = 5.00
	HaikuCacheWritePrice = 1.25 // 1.25x base
	HaikuCacheReadPrice  = 0.10 // 0.1x base
)

// CostBreakdown contains detailed cost information for a completion.
type CostBreakdown struct {
	InputCost      float64
	OutputCost     float64
	CacheWriteCost float64
	CacheReadCost  float64
	TotalCost      float64

	InputTokens      int
	OutputTokens     int
	CacheWriteTokens int
	CacheReadTokens  int
	TotalTokens      int
}

// CalculateCost calculates the cost for a completion based on the model.
func CalculateCost(model string, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int) CostBreakdown {
	var inputPrice, outputPrice, cacheWritePrice, cacheReadPrice float64

	// Determine pricing based on model
	switch {
	case containsString(model, "opus"):
		inputPrice = OpusInputPrice
		outputPrice = OpusOutputPrice
		cacheWritePrice = OpusCacheWritePrice
		cacheReadPrice = OpusCacheReadPrice
	case containsString(model, "sonnet"):
		inputPrice = SonnetInputPrice
		outputPrice = SonnetOutputPrice
		cacheWritePrice = SonnetCacheWritePrice
		cacheReadPrice = SonnetCacheReadPrice
	case containsString(model, "haiku"):
		inputPrice = HaikuInputPrice
		outputPrice = HaikuOutputPrice
		cacheWritePrice = HaikuCacheWritePrice
		cacheReadPrice = HaikuCacheReadPrice
	default:
		// Default to Haiku pricing if unknown
		inputPrice = HaikuInputPrice
		outputPrice = HaikuOutputPrice
		cacheWritePrice = HaikuCacheWritePrice
		cacheReadPrice = HaikuCacheReadPrice
	}

	// Calculate costs (prices are per million tokens)
	breakdown := CostBreakdown{
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		CacheWriteTokens: cacheCreationTokens,
		CacheReadTokens:  cacheReadTokens,
		TotalTokens:      inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens,
	}

	breakdown.InputCost = float64(inputTokens) * inputPrice / 1_000_000
	breakdown.OutputCost = float64(outputTokens) * outputPrice / 1_000_000
	breakdown.CacheWriteCost = float64(cacheCreationTokens) * cacheWritePrice / 1_000_000
	breakdown.CacheReadCost = float64(cacheReadTokens) * cacheReadPrice / 1_000_000
	breakdown.TotalCost = breakdown.InputCost + breakdown.OutputCost + breakdown.CacheWriteCost + breakdown.CacheReadCost

	return breakdown
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr)))
}
