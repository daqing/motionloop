# Phase 2：Tool 接口、最小 agent loop 与首批工具（M0 收口）

> 里程碑：M0（第 3/3 步，**M0 验收在本阶段完成**）｜ 前置：Phase 1 ｜ 状态：未开始

## 目标

`agent` 包最小可用：完整事件定义 + 串行工具执行的循环 + bash/read 两工具 + CLI 单发模式，端到端跑通第一次"模型调工具→回答"。

## 任务清单

- [ ] `agent/tool.go`：`Tool` 接口（`Name()` / `Description()` / `Parameters()` / `Execute(ctx, call, emit)`）、`ToolCall{ID, Name, Arguments json.RawMessage}`、`Result{Content []llm.ContentBlock, Details any, Terminate bool}`、`ErrorResult(err)` 辅助
- [ ] `agent/schema.go`：struct tag → JSON Schema 反射生成（string/number/bool/[]string/嵌套 struct，`required` 与 `description` tag；够用即可，不追求完整规范）
- [ ] `agent/event.go`：完整事件集一次性定义——`agent_start/end`、`turn_start/end`、`message_start/update/end`、`tool_execution_start/update/end`（对齐 PLAN §2 的事件表）+ `Subscribe(func(Event))`
- [ ] `agent/loop.go`：`Agent`（model、tools、messages、thinking level）+ `Prompt(ctx, input)`：
  - user 消息入 transcript → 循环 { `Provider.Stream` → 累积 assistant 消息 → 有 ToolCall 则逐个 `Execute`（本阶段串行）→ toolResult 回填 → 继续下一轮 } → 无 ToolCall / `Terminate` / ctx 取消时结束
  - 工具执行错误 → `isError` 的 toolResult 回填并**继续循环**
  - LLM 错误（`Stop{error}`）→ 结束 run，事件序列保持完整
- [ ] `agent/fakeprovider.go`：脚本化 fake provider（按预设序列返回 assistant 消息），供本阶段及后续所有 loop 测试使用
- [ ] `tools/bash.go`：命令执行（`timeout` 参数、stdout/stderr 合并、`workdir` 参数）
- [ ] `tools/read.go`：行号前缀（`cat -n` 风格、tab 分隔）、默认截断 2000 行、`offset`/`limit` 参数、图片按 ImageBlock 返回
- [ ] `cmd/motionloop`：`-p` 单发模式（provider/model/env key 临时硬编码；配置体系 Phase 8 才有）
- [ ] 测试：fake provider 驱动 loop 五场景（无工具 / 单工具 / 工具错误后继续 / LLM 错误终止 / ctx 取消）；bash、read 各自的临时目录测试

## 设计要点

- 事件集一次到位、hook 留到 Phase 3：事件是 loop 的副产品，先有完整观测面，再谈拦截面。
- `Result.Content` 回传模型、`Details` 只给 UI/日志——这条区分贯穿所有工具实现（PLAN §4.2）。
- 工具参数校验：loop 在 `Execute` 前按 schema 校验 arguments，失败直接生成 isError toolResult，不进工具代码。

## 验收标准（M0，PLAN §12）

- `go run ./cmd/motionloop -p "读一下 PLAN.md 并总结"` 在真实端点上完成一次 read 工具调用并给出回答
- loop 五场景单测全绿

## 验证命令

```bash
go build ./... && go test ./...
MOTIONLOOP_E2E=1 go run ./cmd/motionloop -p "读一下 PLAN.md 并总结"
```
