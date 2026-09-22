# Phase 8：anthropic provider、models.json 与配置体系（M3 收口）

> 里程碑：M3（**M3 验收在本阶段完成**）｜ 前置：Phase 7 ｜ 状态：已完成（2026-09-22，真实端点 e2e 待用户带 key 复验）

## 目标

第二个 provider（Anthropic 原生 Messages API）、内置模型目录、三级配置合并与项目信任机制，"可扩展 LLM provider 插件"的配置面成型（PLAN §4.1 / §10）。

## 任务清单

- [x] `llm/provider/anthropic/`：Messages API 转换（system 收敛为顶层字段并合并多条、空内容 patch 消息不产生请求文本、`tool_use`/`tool_result` 映射、连续同角色消息合并、图片 base64 source、thinking 块回传）；thinking 按 `ThinkingLevel` 映射 `budget_tokens`（仅 thinking 能力模型，max_tokens 自动抬高）；cache 能力模型在最后一块 system 上加 `cache_control: ephemeral`；SSE 事件映射（`content_block_start/delta`、`input_json_delta`、usage 跨 `message_start`/`message_delta` 累计合并后单点上报、`message_stop` 即 DONE、`error` 事件、缺 `message_stop` = interrupted）；`x-api-key` + `anthropic-version` 头；内置模型条目（sonnet/opus/haiku）
- [x] `llm/catalog.go`：`Catalog`（按 provider/model 合并、后写覆盖）+ `ModelsOf` 可选接口断言（phase 文档写的目录实现落于此，与 providers 的聚合在 config 侧完成）
- [x] `config/config.go`：`Settings`（provider/model/profile）三级合并——内置默认 → 全局 settings.json → **受信任的**项目 settings.json → `MOTIONLOOP_*` env；`config/models.go`：models.json 加载注册（openai-compat / anthropic 两种 type、id 冲突 panic 暴露）+ `ResolveAPIKey`（自定义 env → 厂商 env → MOTIONLOOP_API_KEY）+ `ResolveModel`（catalog：自定义优先、builtin 兜底、未知 id 仍给出裸条目）
- [x] `config/trust.go`：`TrustStore`（`~/.motionloop/trust.json`，指纹 = 路径 + git remote origin URL，指纹变化即撤销信任）；CLI `motionloop trust` / `untrust` 子命令；未信任时项目级 settings/prompts 跳过并提示
- [x] CLI：`--provider/--model/--profile` 未指定时回落 settings；cmd 移除本地 key 表与模型解析，统一走 config
- [x] 测试：anthropic 六场景（文本+usage 合并 / thinking+tool_use 分片 / HTTP 错误 / error 事件 / 中断 / 请求形状：system 合并与 cache_control、patch 消息过滤、tool_result user 化、input_schema、头）+ thinking 预算与 max_tokens 联动 + **anthropic loop e2e**（真 provider + 真 read 工具两轮请求）；配置合并优先级表驱动 / models.json 加载注册解析 / key 顺序 / trust 生命周期（持久化、无 remote 不变、增/改 remote 撤销、重信任、解除）+ **models.json 第三方端点 e2e**（本地 SSE 服务器纯靠配置接入，agent 跑通）
- [x] 顺带修复：openaicompat 过滤空内容 system 消息（section patch 消息不再产生空 system 块）

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

## 实施备注（2026-09-22）

- **目录位置偏差**：phase 文档写的 `llm/catalog.go` 只能承载合并逻辑（llm 不能 import provider 子包，否则循环依赖）；providers 聚合经 `ModelsOf` 可选接口在 config 侧完成。
- **anthropic usage 合并**：input/cache 在 `message_start`、output 在 `message_delta`，provider 侧累计后在 `message_delta` 单点发 `UsageUpdate`（避免 loop 取"最后一个"而丢 input）。
- **连续同角色合并**：toolResult 是 user 角色，多个结果合并进同一条 user 消息（Messages API 要求）；openaicompat 无此约束，各自独立。
- **thinking 预算映射**：minimal/low/medium/high → 1024/2048/8192/16384 tokens；仅对声明 thinking 能力的模型启用，且 max_tokens 自动抬到预算 + 4096。
- **trust 指纹**：`sha256(abs path + remote origin URL)`——目录移动、换 remote、去 remote 都会撤销信任（测试覆盖）；`git init`（无 remote）不影响。
- models.json 的 provider id 与内置冲突会 panic（注册表层重复保护），属启动期接线错误，尽早暴露。
- 真实端点 e2e 未跑（无 key）；脚本化替代：anthropic loop e2e + models.json 第三方端点 e2e 覆盖全链路，用户可带 key 复验 `--provider anthropic`。
