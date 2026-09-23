# Changelog

## v0.1.0 (2026-09-23)

First release: the full planned feature set through milestone M5.

- **llm**: unified message model (pi-aligned JSON), stream events, provider
  registry, model catalog; OpenAI-compatible provider (SSE streaming,
  tool-call sharding, usage) and native Anthropic Messages provider
  (thinking budgets, prompt caching, cumulative usage).
- **agent**: the LLM-to-tool loop with a ten-event observation surface,
  BeforeToolCall/AfterToolCall/FinishTurn hooks, TransformContext /
  ConvertToLLM pipeline, parallel tool batches (completion-order events,
  source-order persistence) with per-tool sequential override, steering and
  follow-up queues, mid-session prompt patches and tool-loadout diffs.
- **tools**: bash, read, write, edit, grep (ripgrep with built-in fallback),
  glob (**), ls — anchored to a Workspace with escape detection, read
  tracking against blind overwrites, a mutation lock, head+tail truncation,
  and Coding/Minimal presets; plus skills_load and memory_save.
- **session**: append-only JSONL with the pi-compatible id/parentId entry
  schema, in-place branching, cross-file forking, torn-tail tolerance,
  kill-and-resume, and threshold-driven compaction whose replay view is
  constructionally identical to the live request view.
- **prompt / profile**: named prompt sections with a three-level override
  chain, environment facts (cwd, platform, date, git), and the built-in
  coding profile.
- **skills / memory**: Agent Skills discovery (global, shared ~/.agents,
  trusted project) with progressive disclosure; per-project long-term
  memory with a MEMORY.md index injected as background context.
- **config**: three-level settings merge with project trust fingerprints,
  models.json custom providers, layered API-key resolution.
- **cmd/motionloop**: interactive REPL (/model, /compact, /fork, /exit,
  steering, interrupt semantics), one-shot -p mode with session recording,
  --headless JSON event stream, session ls/show/fork/resume, trust/untrust.

Runtime code has zero third-party dependencies (standard library only).
