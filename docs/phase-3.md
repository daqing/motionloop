# Phase 3：hooks 体系与事件顺序保证

> 里程碑：M1（第 1/4 步）｜ 前置：Phase 2 ｜ 状态：已完成（2026-09-22）

## 目标

补齐 pi 式全部 hook 拦截点与订阅语义，agent core 的"拦截面"成型（PLAN §4.2）。

## 任务清单

- [x] `agent/hook.go`：
  - `BeforeToolCall(ctx, call) → {Block, Reason, Terminate}`——拦截时生成 isError toolResult（空 Reason 落默认文案）
  - `AfterToolCall(ctx, call, result, isError) → AfterToolCallResult`——指针字段表达可选覆盖（Content/Details/IsError/Terminate，nil = 保留）
  - `FinishTurn(ctx, Turn) → DecisionDefault | DecisionEnd | DecisionContinue`
- [x] `Agent` 配置化：五个新 option（`WithBeforeToolCall/WithAfterToolCall/WithFinishTurn/WithTransformContext/WithConvertToLLM`）；`ConvertToLLM` 与 `TransformContext` 默认直通（nil 即不干预）
- [x] `tool_execution_update` 接通（Phase 2 已实现转发，本阶段以含 update 的完整事件 golden 锁定次序）
- [x] 订阅语义：同步回调按注册顺序执行；`agent_end` 后不再有事件（测试锁定）
- [x] 测试：
  - 完整事件顺序 golden（含工具批 start/update×2/end 与 pi 序列对齐）
  - before 拦截（0 次执行、reason 透传、run 继续）与 block+terminate 提前结束
  - after 改写 content/details/isError 生效 + terminate 覆盖结束 run
  - FinishTurn 三分支（default 有结果继续 / end 有结果也停 / continue 无结果补一轮）
  - hook 收到 Prompt 的 ctx；TransformContext 只影响请求侧、不改 transcript

## 设计要点

- hook 契约对齐 pi：不 panic、不阻塞超过 ctx 允许的时间；返回零值 = 不干预。
- `ConvertToLLM` 默认实现 = 过滤非 LLM 消息后直通——这是 Phase 5 session 重放和 Phase 10 compaction 的接入点。

## 验收标准

- 事件顺序 golden 与 PLAN §2 所列 pi 事件序列一致
- 三 hook 行为单测全绿

## 验证命令

```bash
go test -race ./agent/...
```

## 实施备注（2026-09-22）

- **before-hook 时机对齐 pi**：在 `ToolExecutionStart` 事件之后、schema 校验之后运行——校验失败的结果不经过 hook（测试曾因此误报：非法参数在 hook 前就被拦截）。
- **finishTurn 在 `TurnEnd` 事件之前运行**；`allTerminate`（整批 terminate）优先于 decision；error/aborted 轮次是硬退出，不调 finishTurn（对齐 pi）。
- **DecisionContinue** 在无 toolResult 时也会补一轮纯上下文请求；无条件返回 Continue 会造成死循环，已在类型文档标注。
- 请求侧上下文管道：`transcript → TransformContext → ConvertToLLM → provider`，两者默认直通；transcript 永不被管道修改——这是 Phase 5 session 重放与 Phase 10 compaction 的接入点。
- `AfterToolCallResult` 用指针字段做可选覆盖（nil = 保留执行结果），语义对齐 pi 的字段级合并（无深合并）。
