# motionloop

A general-purpose agent framework in Go: bring your own system prompt, a
general tool set, and pluggable LLM providers, and get a complete
"LLM ⇄ tool" loop for any agent scenario — not just coding.

The core coding-agent harness design (agent loop, tool system, JSONL
sessions, compaction, skills, memory) follows
[pi](https://github.com/earendil-works/pi); motionloop is framework-first
— the CLI is just the largest consumer of the library. Runtime code has
zero third-party dependencies (standard library only).

## Install

Requires Go 1.27+.

```bash
go install github.com/daqing/motionloop/cmd/motionloop@latest
```

Or from a checkout:

```bash
go build ./...
go test ./...
```

## The library in five minutes

Assemble an agent from a provider, a model, tools, and a system prompt:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	_ "github.com/daqing/motionloop/llm/provider/openaicompat" // register providers
	"github.com/daqing/motionloop/tools"
)

func main() {
	provider, err := llm.Resolve("glm") // or "openai", "deepseek", "anthropic"
	if err != nil {
		panic(err)
	}
	model := llm.Model{ProviderID: "glm", ModelID: "glm-4.6"}

	ws, _ := tools.NewWorkspace(".")
	a := agent.New(provider, model,
		agent.WithTools(tools.Minimal(ws)...), // bash + read + write
		agent.WithSystemPrompt("You are a helpful assistant."),
		agent.WithStreamOptions(llm.StreamOptions{APIKey: os.Getenv("GLM_API_KEY")}),
	)

	// stream the run: text deltas, tool executions, usage
	a.Subscribe(func(ev agent.Event) {
		if u, ok := ev.(agent.MessageUpdate); ok {
			if d, ok := u.Delta.(llm.TextDelta); ok {
				fmt.Print(d.Delta)
			}
		}
	})

	if err := a.Prompt(context.Background(), "list the Go files here"); err != nil {
		panic(err)
	}
}
```

One `Provider` interface covers every OpenAI-compatible endpoint (OpenAI,
GLM, DeepSeek, Kimi, Ollama, vLLM, …) plus a native Anthropic Messages
provider. Define your own tools by implementing one interface — arguments
are plain structs with `json`/`jsonschema` tags, and schema generation plus
validation come for free. For tests, `agent.NewFakeProvider` scripts
responses without any network.

## The CLI

```bash
export GLM_API_KEY=...            # or OPENAI_API_KEY, ANTHROPIC_API_KEY, MOTIONLOOP_API_KEY

motionloop                         # interactive REPL: /model, /compact, /fork, /exit
motionloop -p "fix the failing test and run it"
motionloop --provider anthropic --model claude-sonnet-4-5 -p "..."
motionloop --headless -p "..."     # one JSON event per line, for embedding

motionloop trust                   # allow project-level config in this repo
motionloop session ls              # list recorded sessions
motionloop session show <id>       # entry tree with the current chain marked
motionloop session fork <id>       # branch a session
motionloop session resume <id> -p "continue where we left off"
```

Sessions record to `~/.motionloop/sessions/` as append-only JSONL with a
pi-compatible entry schema (an id/parentId tree): kill the process and
`resume` picks the conversation back up; `fork` branches without copying.

## Configuration

- `~/.motionloop/settings.json` — defaults per user: `provider`, `model`,
  `profile`, `compactionThreshold` (estimated tokens; negative disables
  compaction). Project `.motionloop/settings.json` loads once trusted;
  `MOTIONLOOP_PROVIDER` / `MOTIONLOOP_MODEL` / `MOTIONLOOP_PROFILE` win over
  files; CLI flags win over everything.
- `~/.motionloop/models.json` — custom providers without writing Go:

  ```json
  {
    "providers": [
      {
        "id": "myllm",
        "baseUrl": "http://localhost:11434/v1",
        "apiKeyEnv": "MYLLM_KEY",
        "type": "openai-compat",
        "models": [{ "id": "llama-x", "contextWindow": 131072, "images": true }]
      }
    ]
  }
  ```

- `~/.motionloop/prompts/coding/<section>.md` — override any system-prompt
  section (`identity`, `rules`, …); trusted projects override in turn via
  `.motionloop/prompts/coding/`.
- Skills: drop [Agent Skills](https://agentskills.io) packages into
  `~/.agents/skills/` (shared across harnesses) or
  `~/.motionloop/skills/`; trusted projects can add `.agents/skills/`.
  Motionloop advertises the index in the system prompt and loads bodies on
  demand via the `skills_load` tool.
- Memory: durable facts live in `~/.motionloop/memory/<project>/` (one file
  per fact) with a `MEMORY.md` index injected as background context; the
  `memory_save` tool records preferences, corrections, and project
  constraints.

## Packages

```
cmd/motionloop    CLI: REPL, one-shot, headless, session management
llm/              message model, stream events, provider registry
llm/provider/     openaicompat and anthropic providers
agent/            the loop: tools, events, hooks, steering, compaction glue
tools/            bash, read, write, edit, grep, glob, ls (+ skills_load,
                  memory_save), workspace safety, Coding/Minimal presets
prompt/           named prompt sections with override chain
profile/coding/   the built-in coding profile
session/          JSONL sessions (pi entry schema), fork/branch, compaction
skills/           Agent Skills discovery and index
memory/           per-project long-term memory store
config/           settings merge, models.json, project trust
```

Dependency direction is strictly one-way:
`cmd → profile → (prompt, tools, skills, memory, session) → agent → llm`.

## Status

v0.1 — the planned feature set through M5 is complete; see
[PLAN.md](PLAN.md) for the architecture and decision log and
[docs/](docs/) for the phase-by-phase development history. Real-endpoint
e2e runs are opt-in (`MOTIONLOOP_E2E=1`). MIT licensed — see
[LICENSE](LICENSE).
