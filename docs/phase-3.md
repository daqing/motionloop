# Phase 3：hooks 体系与事件顺序保证

> 里程碑：M1（第 1/4 步）｜ 前置：Phase 2 ｜ 状态：未开始

## 目标

补齐 pi 式全部 hook 拦截点与订阅语义，agent core 的"拦截面"成型（PLAN §4.2）。

## 任务清单

- [ ] `agent/hook.go`：
  - `BeforeToolCall(ctx, call, args) → {Block bool, Reason string, Terminate bool}`——可拦截（拦截时生成 isError toolResult）
  - `AfterToolCall(ctx, call, result) → Result`——可改写 content/details/isError
  - `FinishTurn(turn) → {End | Continue | Default}`——run 终止与补一轮决策
- [ ] `Agent` 配置化：functional options（挂 hooks、`ConvertToLLM`、`TransformContext`——后两者本阶段只留接口与默认直通实现）
- [ ] `tool_execution_update` 接通：工具 `Execute` 的 `emit(Update)` 回调转发给订阅者（流式进度，如 bash 增量输出）
- [ ] 订阅语义：同步回调按注册顺序执行；`agent_end` 后不再有事件
- [ ] 测试：
  - 完整事件顺序 golden 测试（含工具批的 start/update/end 次序，对齐 pi 序列）
  - before 拦截 → isError 结果且带 reason；after 改写 content 生效；FinishTurn 的 end / continue / default 三分支
  - `Terminate` 提示：整批全部 terminate 才提前结束（对齐 pi 语义）

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
