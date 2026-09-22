# motionloop

A general-purpose agent framework in Go: bring your own system prompt, a
general tool set, and pluggable LLM providers, and get a complete
"LLM ⇄ tool" loop for any agent scenario — not just coding.

The core coding-agent harness design (agent loop, tool system, sessions,
compaction, skills, memory) follows [pi](https://github.com/earendil-works/pi),
but motionloop is framework-first: the CLI is just the largest consumer of
the library.

> **Status: early development.** The message model and stream event
> vocabulary (`llm` package) are in place; the agent loop, tools, sessions,
> and CLI arrive over the next phases. See [PLAN.md](PLAN.md) and
> [docs/](docs/) for the full roadmap.

## Goals

- **Custom system prompts** — template with named sections, three-level
  overrides (built-in → global → project), dynamic environment facts.
- **General tool set** — `bash`, `read`, `write`, `edit`, `grep`, `glob`,
  `ls` shipped as presets (`coding` / `minimal` / empty); register your own
  tools by implementing one interface.
- **Pluggable LLM providers** — one OpenAI-compatible provider covers most
  endpoints (OpenAI, GLM, DeepSeek, Kimi, Ollama, vLLM, …), plus a
  native Anthropic provider and config-driven custom entries.

## Non-goals (for v0.x)

No subprocess plugin protocols (MCP-style), no built-in sandboxing (the
agent runs with your permissions), no TUI — see the decision log in
[PLAN.md](PLAN.md#15-决策记录原开放问题review-已全部决策).

## Getting started

Requires Go 1.27+.

```bash
go build ./...
go test ./...
go run ./cmd/motionloop --version
```

## Architecture

```
cmd/motionloop/   CLI entry (thin assembly)
llm/              unified LLM vocabulary: messages, content blocks,
                 stream events, model metadata, provider registry
```

Upcoming packages (see [PLAN.md](PLAN.md)): `agent` (loop, tools, hooks,
events), `tools`, `prompt`, `profile`, `session` (JSONL, pi-compatible
entry schema), `skills`, `memory`, `config`.

Dependency direction is strictly one-way:
`cmd → profile → (prompt, tools, skills, memory, session) → agent → llm`.

Two invariants worth knowing early:

- Message and content-block JSON field names are aligned with pi's session
  schema, so sessions persist messages verbatim. Do not rename them.
- Runtime code carries zero third-party dependencies — standard library
  only.

## Documentation

- [PLAN.md](PLAN.md) — architecture and all decided questions
- [docs/phase-0.md](docs/phase-0.md) … [docs/phase-11.md](docs/phase-11.md)
  — step-by-step development plan with acceptance criteria per phase
- [AGENTS.md](AGENTS.md) — conventions for agents contributing to this repo
