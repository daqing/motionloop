package openaicompat

import "github.com/daqing/motionloop/llm"

// Endpoint binds a provider identity to an OpenAI-compatible base URL and
// model catalog.
type Endpoint struct {
	ProviderID string
	BaseURL    string
	Models     []llm.Model
}

// BuiltinEndpoints registers one provider identity per supported vendor
// out of the box. Custom endpoints are added via New or, from Phase 8 on,
// via models.json.
var BuiltinEndpoints = []Endpoint{
	{
		ProviderID: "openai",
		BaseURL:    "https://api.openai.com/v1",
		Models: []llm.Model{
			{ProviderID: "openai", ModelID: "gpt-5.2", ContextWindow: 400_000, MaxOutput: 128_000, Capabilities: llm.Capabilities{Thinking: true, Images: true, Cache: true, ParallelToolCalls: true}},
			{ProviderID: "openai", ModelID: "gpt-5.2-mini", ContextWindow: 400_000, MaxOutput: 128_000, Capabilities: llm.Capabilities{Thinking: true, Images: true, Cache: true, ParallelToolCalls: true}},
		},
	},
	{
		ProviderID: "deepseek",
		BaseURL:    "https://api.deepseek.com/v1",
		Models: []llm.Model{
			{ProviderID: "deepseek", ModelID: "deepseek-chat", ContextWindow: 128_000, MaxOutput: 8_192, Capabilities: llm.Capabilities{ParallelToolCalls: true}},
			{ProviderID: "deepseek", ModelID: "deepseek-reasoner", ContextWindow: 128_000, MaxOutput: 8_192, Capabilities: llm.Capabilities{Thinking: true, ParallelToolCalls: true}},
		},
	},
	{
		ProviderID: "glm",
		BaseURL:    "https://open.bigmodel.cn/api/paas/v4",
		Models: []llm.Model{
			{ProviderID: "glm", ModelID: "glm-4.6", ContextWindow: 200_000, MaxOutput: 32_768, Capabilities: llm.Capabilities{Thinking: true, Images: true, ParallelToolCalls: true}},
			{ProviderID: "glm", ModelID: "glm-4.6-air", ContextWindow: 128_000, MaxOutput: 16_384, Capabilities: llm.Capabilities{Thinking: true, Images: true, ParallelToolCalls: true}},
		},
	},
}

func init() {
	for _, e := range BuiltinEndpoints {
		llm.Register(New(e.ProviderID, e.BaseURL, e.Models))
	}
}
