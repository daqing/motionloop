package llm

// Capabilities declares what a model or provider supports.
type Capabilities struct {
	Thinking          bool `json:"thinking"`
	Images            bool `json:"images"`
	Cache             bool `json:"cache"`
	ParallelToolCalls bool `json:"parallelToolCalls"`
}

// Model identifies one addressable LLM and its limits.
type Model struct {
	ProviderID    string       `json:"providerId"`
	ModelID       string       `json:"modelId"`
	ContextWindow int64        `json:"contextWindow"`
	MaxOutput     int64        `json:"maxOutput"`
	Capabilities  Capabilities `json:"capabilities"`
}

// String returns the "provider/model" identifier.
func (m Model) String() string {
	return m.ProviderID + "/" + m.ModelID
}
