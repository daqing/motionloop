# motionloop

用 Go 实现的通用 Agent 框架：自定义系统提示词 + 通用 tool 工具集 + 可插拔的
LLM provider，得到一个适用于任意 agent 场景（而不止编码）的完整
"LLM ⇄ 工具" 循环。

核心 coding agent harness 设计（agent loop、tool 体系、session、compaction、
skills、memory）参考 [pi](https://github.com/earendil-works/pi) 的实现，但
motionloop 是**框架优先**：CLI 只是这个库的最大号使用者。

> **状态：早期开发中。** 消息模型与流事件词汇表（`llm` 包）已就位；agent
> loop、工具集、session 与 CLI 将随后续阶段逐步交付。完整路线见
> [PLAN.md](PLAN.md) 与 [docs/](docs/)。

## 目标

- **可自定义系统提示词** —— 命名分区的模板、三级覆盖（内置 → 全局 →
  项目）、动态注入环境事实。
- **通用工具集** —— `bash`、`read`、`write`、`edit`、`grep`、`glob`、`ls`
  按 preset 提供（`coding` / `minimal` / 空集）；实现一个接口即可注册自己的
  工具。
- **可插拔 LLM provider** —— 一个 OpenAI 兼容 provider 覆盖大多数端点
  （OpenAI、GLM、DeepSeek、Kimi、Ollama、vLLM 等），外加 Anthropic 原生
  provider 与配置驱动的自定义条目。

## Non-goals（v0.x 不做）

不做子进程插件协议（MCP 之类）、不做内置沙箱（agent 以当前用户权限运行）、
不做 TUI —— 决策记录见
[PLAN.md](PLAN.md#15-决策记录原开放问题review-已全部决策)。

## 快速开始

要求 Go 1.27+。

```bash
go build ./...
go test ./...
go run ./cmd/motionloop --version
```

## 架构

```
cmd/motionloop/   CLI 入口（薄壳装配）
llm/              统一 LLM 词汇表：消息、内容块、流事件、
                 模型元数据、provider 注册表
```

后续包（见 [PLAN.md](PLAN.md)）：`agent`（loop、工具、hooks、事件）、
`tools`、`prompt`、`profile`、`session`（JSONL，pi 兼容 entry schema）、
`skills`、`memory`、`config`。

依赖方向严格单向：
`cmd → profile → (prompt, tools, skills, memory, session) → agent → llm`。

两条值得尽早知道的不变量：

- 消息与内容块的 JSON 字段名与 pi 的 session schema 对齐，session 落盘零
  转换，**不要重命名**。
- 运行时代码零第三方依赖——仅标准库。

## 文档

- [PLAN.md](PLAN.md) —— 总体架构与全部已决策问题
- [docs/phase-0.md](docs/phase-0.md) … [docs/phase-11.md](docs/phase-11.md)
  —— 分阶段开发计划，每阶段附验收标准
- [AGENTS.md](AGENTS.md) —— 参与本项目（含 agent）的协作约定
