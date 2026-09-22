# Phase 8：anthropic provider、models.json 与配置体系（M3 收口）

> 里程碑：M3（**M3 验收在本阶段完成**）｜ 前置：Phase 7 ｜ 状态：未开始

## 目标

第二个 provider（Anthropic 原生 Messages API）、内置模型目录、三级配置合并与项目信任机制，"可扩展 LLM provider 插件"的配置面成型（PLAN §4.1 / §10）。

## 任务清单

- [ ] `llm/provider/anthropic/`：
  - Messages API：system 独立字段、content blocks 映射、`tool_use` / `tool_result` 语义、thinking（`thinking.budget_tokens`）、prompt caching（`cache_control`）
  - SSE 流式：`content_block_delta` 系列事件 → motionloop StreamEvent
  - 错误语义与 openaicompat 一致：`Stop{error}`，不抛
- [ ] `llm/catalog.go`：内置模型目录（openaicompat + anthropic 条目统一来源）；`models.json` 条目合并覆盖内置
- [ ] `config/config.go`：
  - 三级合并：内置默认 → `~/.motionloop/settings.json` → `.motionloop/settings.json`（项目级）；env `MOTIONLOOP_*` 优先级最高
  - `models.json`：自定义 provider / model 条目（`baseURL`、`apiKeyEnv`、模型列表、能力声明、上下文窗口）
  - API key 解析顺序：models.json 指定的 env 名 → 厂商惯例 env（`ANTHROPIC_API_KEY` / `OPENAI_API_KEY`）
- [ ] `config/trust.go`：项目信任——首次进入项目记录指纹（路径 + git remote），未信任不加载项目级配置 / prompts / skills；`motionloop trust` / `untrust` 子命令
- [ ] CLI：`--provider` / `--model` 全局 flag 接配置解析
- [ ] 测试：anthropic fake SSE 回放（文本 / thinking / tool_use / 缓存头 / 错误）；配置合并优先级表驱动测试；trust 状态机（首次询问 → 记住 → 指纹变化重新询问）

## 设计要点

- provider 能力差异收敛在 provider 内部（如 anthropic 的 system 是顶层字段、tool call 无 index 分片），`llm.Message` 模型不感知。
- models.json 是"不开 Go 代码加 provider"的唯一入口，schema 要向后兼容（未知字段忽略）。

## 验收标准（M3，PLAN §12）

- 双 provider 各跑通 e2e
- 仅靠 `models.json` 接入一个第三方 OpenAI 兼容端点（本地 ollama 或任意云服务）

## 验证命令

```bash
go test ./llm/... ./config/...
MOTIONLOOP_E2E=1 go run ./cmd/motionloop --provider anthropic -p "..."
```
