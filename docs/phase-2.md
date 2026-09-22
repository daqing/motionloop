# Phase 2：Tool 接口、最小 agent loop 与首批工具（M0 收口）

> 里程碑：M0（第 3/3 步，**M0 验收在本阶段完成**）｜ 前置：Phase 1 ｜ 状态：已完成（2026-09-22，真实端点 e2e 待用户带 key 复验）

## 目标

`agent` 包最小可用：完整事件定义 + 串行工具执行的循环 + bash/read 两工具 + CLI 单发模式，端到端跑通第一次"模型调工具→回答"。

## 任务清单

- [x] `agent/tool.go`：`Tool` 接口（`Name()` / `Description()` / `Parameters()` / `Execute(ctx, call, emit)`）、`ToolCall{ID, Name, Arguments json.RawMessage}`、`Result{Content []llm.ContentBlock, Details any, Terminate bool}`、`ErrorResult(err)` 辅助、`Update` 流式部分结果
- [x] `agent/schema.go`：struct tag → JSON Schema 反射生成（string/integer/number/bool/[]T/嵌套 struct/map，`required` 与 `description=` tag）+ `Validate`（含 required、类型、嵌套、数组元素）
- [x] `agent/event.go`：完整事件集一次性定义——`agent_start/end`、`turn_start/end`、`message_start/update/end`、`tool_execution_start/update/end` + `Subscribe(func(Event))`
- [x] `agent/loop.go`：`Agent` + functional options（`WithTools/WithSystemPrompt/WithMessages/WithStreamOptions/WithThinkingLevel`）+ `Prompt(ctx, input)` 串行循环；工具错误继续、LLM 错误结束但事件序列完整（`ErrStreamFailed`/`ErrAborted`/ctx 取消三分）、`Terminate` 整批生效
- [x] `agent/fakeprovider.go`：脚本化 fake provider（`NewFakeProvider` + `FakeTextEvents/FakeToolCallEvents/FakeErrorEvents` 辅助 + 请求记录）
- [x] `tools/bash.go`：命令执行（`timeout`/`workdir` 参数、合并输出、非零退出码附在输出、64KB 截断）
- [x] `tools/read.go`：行号前缀（tab 分隔）、默认 2000 行、`offset`/`limit`、图片扩展名 → ImageBlock（base64）、截断/越界提示
- [x] `cmd/motionloop`：`-p` 单发 + `--provider`/`--model` + env key 解析（OPENAI/DEEPSEEK/GLM_API_KEY，`MOTIONLOOP_API_KEY` 兜底）+ 行式事件渲染（文本到 stdout、工具活动到 stderr、usage 汇总）
- [x] 测试：loop 场景（无工具 / 单工具含事件顺序 golden / 工具错误后继续 / LLM 错误终止 / ctx 取消前后 / 未知工具 / 非法参数 / Terminate）+ schema 生成与校验 + bash/read 临时目录测试 + **集成测试**（真 openaicompat provider + 真 SSE 服务器 + 真 read 工具走完整两轮请求）

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

## 实施备注（2026-09-22）

- **jsonschema tag 约定**：`description=` 必须是最后一个 token，其后内容（含逗号）整体视为描述——修复了描述含逗号被拆碎的解析问题。
- **bash 语义**：非零退出码是**正常结果**（输出尾部附 `Exit code: N`，由模型自行反应），只有命令无法启动才走 isError；timeout 经 `exec.CommandContext` 实现，超时与退出码都写入 `Details`。
- **事件顺序对齐 pi**：`ToolExecutionEnd` 先于 toolResult 的 `MessageStart/End`；system 消息在首个 turn 内发出（`TurnStart` 之后、user 之前）。
- **校验先于执行**：loop 在 `Execute` 前按 schema 校验 arguments，未知工具/非法参数直接生成 isError 结果，工具代码不会被执行（测试断言 0 次执行）。
- **真实端点 e2e 未跑**（本机 shell 无任何 API key）：以 `agent/integration_test.go` 替代——真 openaicompat provider + httptest SSE 服务器 + 真 read 工具，完整验证两轮请求（工具声明、tool_call 分片、tool 回传）。用户可随时执行：
  `GLM_API_KEY=... MOTIONLOOP_E2E=1 go run ./cmd/motionloop --provider glm -p "读一下 PLAN.md 并总结"`
- CLI 的 `--model` 缺省取 provider 目录第一条；base URL 覆盖等完整配置面留给 Phase 8。
