// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package anthropic

// ModelTier represents a classification of model capabilities.
type ModelTier string

const (
	// ModelFastCheap represents the fastest, most cost-effective model.
	// Currently maps to Claude Haiku 4.5.
	ModelFastCheap ModelTier = "fast-cheap"

	// ModelBalanced represents a balanced model (good speed and intelligence).
	// Currently maps to Claude Sonnet 4.6.
	ModelBalanced ModelTier = "balanced"

	// ModelFlagship represents the most capable model.
	// Currently maps to Claude Opus 4.6.
	ModelFlagship ModelTier = "flagship"
)

// GetModel returns the actual model ID for a given tier.
func GetModel(tier ModelTier) string {
	switch tier {
	case ModelFastCheap:
		return "claude-haiku-4-5"
	case ModelBalanced:
		return "claude-sonnet-4-6"
	case ModelFlagship:
		return "claude-opus-4-6"
	default:
		return "claude-haiku-4-5"
	}
}

// GetMinCacheTokens returns the minimum number of tokens required for caching.
func GetMinCacheTokens(model string) int {
	switch model {
	case "claude-opus-4-6":
		return 4096
	case "claude-sonnet-4-6":
		return 2048
	case "claude-haiku-4-5", "claude-haiku-4-5-20251001":
		return 4096
	default:
		return 2048
	}
}
