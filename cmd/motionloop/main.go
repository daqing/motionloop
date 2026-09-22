package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	_ "github.com/daqing/motionloop/llm/provider/openaicompat"
	"github.com/daqing/motionloop/tools"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const defaultSystemPrompt = `You are motionloop, a coding agent.
Inspect files with the read tool and run commands with bash before answering.
Keep answers concise.`

// apiKeyEnv maps provider IDs to their conventional API key variables;
// MOTIONLOOP_API_KEY works for any provider as a fallback.
var apiKeyEnv = map[string]string{
	"openai":   "OPENAI_API_KEY",
	"deepseek": "DEEPSEEK_API_KEY",
	"glm":      "GLM_API_KEY",
}

type modelCatalog interface{ Models() []llm.Model }

func main() {
	prompt := flag.String("p", "", "one-shot prompt (non-interactive)")
	providerID := flag.String("provider", "openai", "provider id")
	modelID := flag.String("model", "", "model id (defaults to the provider's first catalog entry)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("motionloop " + version)
		return
	}
	if *prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: motionloop -p <prompt> [--provider id] [--model id]")
		fmt.Fprintln(os.Stderr, "       motionloop --version")
		os.Exit(2)
	}

	if err := run(context.Background(), *providerID, *modelID, *prompt); err != nil {
		fmt.Fprintln(os.Stderr, "motionloop:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, providerID, modelID, prompt string) error {
	provider, err := llm.Resolve(providerID)
	if err != nil {
		return err
	}
	model, err := resolveModel(provider, modelID)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "motionloop:", model.String())

	opts := llm.StreamOptions{APIKey: os.Getenv("MOTIONLOOP_API_KEY")}
	if env := apiKeyEnv[providerID]; env != "" && opts.APIKey == "" {
		opts.APIKey = os.Getenv(env)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	a := agent.New(provider, model,
		agent.WithTools(tools.Bash{}, tools.Read{Root: cwd}),
		agent.WithSystemPrompt(defaultSystemPrompt),
		agent.WithStreamOptions(opts),
	)
	a.Subscribe(renderEvent)
	return a.Prompt(ctx, prompt)
}

func resolveModel(provider llm.Provider, modelID string) (llm.Model, error) {
	if modelID == "" {
		if catalog, ok := provider.(modelCatalog); ok {
			if models := catalog.Models(); len(models) > 0 {
				return models[0], nil
			}
		}
		return llm.Model{}, fmt.Errorf("provider %q has no default model; pass --model", provider.ID())
	}
	return llm.Model{ProviderID: provider.ID(), ModelID: modelID}, nil
}

// renderEvent prints assistant text to stdout and tool activity to stderr —
// the line-oriented contract until a richer UI arrives.
func renderEvent(ev agent.Event) {
	switch e := ev.(type) {
	case agent.MessageUpdate:
		if d, ok := e.Delta.(llm.TextDelta); ok {
			fmt.Print(d.Delta)
		}
	case agent.MessageEnd:
		if e.Message.Role == llm.RoleAssistant {
			if s := assistantText(e.Message); s != "" && !strings.HasSuffix(s, "\n") {
				fmt.Println()
			}
		}
	case agent.ToolExecutionStart:
		fmt.Fprintf(os.Stderr, "[tool] %s %s\n", e.ToolName, string(e.Arguments))
	case agent.ToolExecutionEnd:
		if e.IsError {
			fmt.Fprintf(os.Stderr, "[tool] %s: error\n", e.ToolName)
		}
	case agent.AgentEnd:
		if last := lastAssistant(e.Messages); last != nil && last.Usage != nil {
			fmt.Fprintf(os.Stderr, "[usage] in=%d out=%d total=%d\n",
				last.Usage.Input, last.Usage.Output, last.Usage.TotalTokens)
		}
	}
}

func assistantText(m llm.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if t, ok := b.(llm.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

func lastAssistant(msgs []llm.Message) *llm.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleAssistant {
			return &msgs[i]
		}
	}
	return nil
}
