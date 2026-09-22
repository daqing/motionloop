package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/llm/provider/anthropic"
	"github.com/daqing/motionloop/llm/provider/openaicompat"
)

// ModelsFile is the models.json schema: custom providers merged into the
// registry, with their entries also feeding the catalog.
type ModelsFile struct {
	Providers []CustomProvider `json:"providers"`
}

// CustomProvider declares one config-driven provider identity.
type CustomProvider struct {
	ID        string        `json:"id"`
	BaseURL   string        `json:"baseUrl"`
	APIKeyEnv string        `json:"apiKeyEnv"`
	Type      string        `json:"type"` // "openai-compat" (default) | "anthropic"
	Models    []CustomModel `json:"models"`
}

// CustomModel declares one model entry.
type CustomModel struct {
	ID            string `json:"id"`
	ContextWindow int64  `json:"contextWindow"`
	MaxOutput     int64  `json:"maxOutput"`
	Thinking      bool   `json:"thinking,omitempty"`
	Images        bool   `json:"images,omitempty"`
	Cache         bool   `json:"cache,omitempty"`
}

// CustomEnvs maps custom provider ids to their apiKeyEnv.
type CustomEnvs = map[string]string

// LoadModels reads models.json (a missing file is an empty config),
// registers its providers, and returns a catalog seeded with their model
// entries. Provider ids must not collide with already-registered
// providers.
func LoadModels(path string) (*llm.Catalog, CustomEnvs, error) {
	envs := CustomEnvs{}
	catalog := llm.NewCatalog()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return catalog, envs, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("config: read models: %w", err)
	}
	var mf ModelsFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	for _, cp := range mf.Providers {
		if cp.ID == "" || cp.BaseURL == "" {
			return nil, nil, fmt.Errorf("config: custom provider needs id and baseUrl")
		}
		models := make([]llm.Model, 0, len(cp.Models))
		for _, cm := range cp.Models {
			if cm.ID == "" {
				return nil, nil, fmt.Errorf("config: provider %q has a model without id", cp.ID)
			}
			models = append(models, llm.Model{
				ProviderID:    cp.ID,
				ModelID:       cm.ID,
				ContextWindow: cm.ContextWindow,
				MaxOutput:     cm.MaxOutput,
				Capabilities: llm.Capabilities{
					Thinking:          cm.Thinking,
					Images:            cm.Images,
					Cache:             cm.Cache,
					ParallelToolCalls: true,
				},
			})
		}
		var p llm.Provider
		switch cp.Type {
		case "", "openai-compat":
			p = openaicompat.New(cp.ID, cp.BaseURL, models)
		case "anthropic":
			p = anthropic.New(cp.ID, cp.BaseURL, models)
		default:
			return nil, nil, fmt.Errorf("config: provider %q has unknown type %q", cp.ID, cp.Type)
		}
		llm.Register(p) // panics on id collision with built-ins or earlier entries
		catalog.Add(models)
		envs[cp.ID] = cp.APIKeyEnv
	}
	return catalog, envs, nil
}

// vendorKeyEnv maps built-in provider ids to their conventional key
// variables.
var vendorKeyEnv = map[string]string{
	"openai":    "OPENAI_API_KEY",
	"anthropic": "ANTHROPIC_API_KEY",
	"deepseek":  "DEEPSEEK_API_KEY",
	"glm":       "GLM_API_KEY",
}

// ResolveAPIKey finds the key for one provider: the models.json apiKeyEnv
// first, then the vendor-conventional variable, then MOTIONLOOP_API_KEY.
func ResolveAPIKey(providerID, customEnv string) string {
	get := os.Getenv
	if customEnv != "" {
		if k := get(customEnv); k != "" {
			return k
		}
	}
	if env := vendorKeyEnv[providerID]; env != "" {
		if k := get(env); k != "" {
			return k
		}
	}
	return get("MOTIONLOOP_API_KEY")
}

// ResolveModel picks the model entry for one provider: an explicit id
// resolved against the catalog (custom entries first, then the provider's
// built-ins), defaulting to the provider's first catalog entry when
// unspecified. Unknown ids still resolve to a bare entry.
func ResolveModel(provider llm.Provider, modelID string, custom *llm.Catalog) (llm.Model, error) {
	catalog := llm.NewCatalog()
	catalog.Add(custom.Models())
	catalog.Add(llm.ModelsOf(provider))
	if modelID == "" {
		if entries := catalog.Models(); len(entries) > 0 {
			return entries[0], nil
		}
		return llm.Model{}, fmt.Errorf("provider %q has no default model; pass --model", provider.ID())
	}
	if m, ok := catalog.Resolve(provider.ID(), modelID); ok {
		return m, nil
	}
	return llm.Model{ProviderID: provider.ID(), ModelID: modelID}, nil
}
