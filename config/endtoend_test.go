package config_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/config"
	"github.com/daqing/motionloop/llm"
)

// TestModelsJSONCustomEndpoint is the M3 acceptance path: a third-party
// OpenAI-compatible endpoint is wired in purely via models.json (here
// pointed at a local SSE server) and a real agent run completes through
// it.
func TestModelsJSONCustomEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer from custom endpoint\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":4,\"total_tokens\":7}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.json")
	if err := os.WriteFile(modelsPath, []byte(`{
		"providers": [
			{
				"id": "thirdparty",
				"baseUrl": "`+srv.URL+`",
				"apiKeyEnv": "THIRDPARTY_KEY",
				"models": [{"id": "custom-1", "contextWindow": 32000}]
			}
		]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THIRDPARTY_KEY", "test-key")

	catalog, envs, err := config.LoadModels(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.Resolve("thirdparty")
	if err != nil {
		t.Fatal(err)
	}
	model, err := config.ResolveModel(provider, "custom-1", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if model.ContextWindow != 32000 {
		t.Fatalf("model = %+v", model)
	}

	a := agent.New(provider, model,
		agent.WithStreamOptions(llm.StreamOptions{APIKey: config.ResolveAPIKey("thirdparty", envs["thirdparty"])}),
	)
	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	msgs := a.Messages()
	var text string
	for _, b := range msgs[1].Content {
		if tb, ok := b.(llm.TextBlock); ok {
			text = tb.Text
		}
	}
	if text != "answer from custom endpoint" {
		t.Fatalf("final text = %q", text)
	}
	if msgs[1].Usage == nil || msgs[1].Usage.TotalTokens != 7 {
		t.Fatalf("usage = %+v", msgs[1].Usage)
	}
}
