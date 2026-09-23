package llm

import "time"

// ThinkingLevel selects a reasoning effort tier.
type ThinkingLevel string

// Reasoning effort tiers, aligned with pi's levels.
const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
)

// Credentials carry the endpoint identity for one provider request.
type Credentials struct {
	// APIKey is sent as the bearer token (Authorization for
	// OpenAI-compatible endpoints, x-api-key for Anthropic). Empty means
	// no auth header, which is correct for keyless endpoints such as
	// local Ollama.
	APIKey string
	// BaseURL overrides the provider's default endpoint base.
	BaseURL string
}

// StreamOptions configures one provider request. The zero value asks for a
// plain streaming completion with provider defaults.
type StreamOptions struct {
	Credentials Credentials
	// MaxTokens caps output tokens; 0 uses the endpoint default.
	MaxTokens int64
	// Temperature is omitted when zero.
	Temperature float64
	// ThinkingLevel maps to the endpoint's reasoning effort knob.
	ThinkingLevel ThinkingLevel
	// Timeout bounds the whole request including body streaming; 0 means
	// only the context bounds it.
	Timeout time.Duration
	// Tools declares callable tools for this request.
	Tools []ToolDecl
}
