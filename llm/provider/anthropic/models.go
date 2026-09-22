package anthropic

import "github.com/daqing/motionloop/llm"

func init() {
	llm.Register(New("anthropic", "https://api.anthropic.com", BuiltinModels))
}

// BuiltinModels lists the built-in catalog entries.
var BuiltinModels = []llm.Model{
	{
		ProviderID: "anthropic", ModelID: "claude-sonnet-4-5",
		ContextWindow: 200_000, MaxOutput: 64_000,
		Capabilities: llm.Capabilities{Thinking: true, Images: true, Cache: true, ParallelToolCalls: true},
	},
	{
		ProviderID: "anthropic", ModelID: "claude-opus-4-3",
		ContextWindow: 200_000, MaxOutput: 64_000,
		Capabilities: llm.Capabilities{Thinking: true, Images: true, Cache: true, ParallelToolCalls: true},
	},
	{
		ProviderID: "anthropic", ModelID: "claude-haiku-4-5",
		ContextWindow: 200_000, MaxOutput: 32_000,
		Capabilities: llm.Capabilities{Images: true, Cache: true, ParallelToolCalls: true},
	},
}
