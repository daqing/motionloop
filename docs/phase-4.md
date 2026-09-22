# Phase 4：并行工具执行与 steering

> 里程碑：M1（第 2/4 步）｜ 前置：Phase 3 ｜ 状态：已完成（2026-09-22）

## 目标

工具批默认并行执行 + per-tool 串行覆盖 + 运行中途插入用户消息（steering / followUp）。

## 任务清单

- [x] `agent/loop.go`：批执行重构为**预检 → 执行 → 汇聚**三段——`ToolExecutionStart` 全批按源序在预检段发出（被拦/校验失败的当场 End），执行段并发跑（`sync.WaitGroup`），`ToolExecutionEnd` 按**完成序**、toolResult 消息按 **assistant 源序**入 transcript
- [x] `ExecutionMode`：以**可选接口**实现（`ExecutionMode() ExecutionMode`），未实现的工具默认并行；批内任一 `ExecutionSequential` → 整批退化为串行
- [x] steering / followUp：`Steer(text)` 入队（one-at-a-time，轮次边界注入、注入一次即出队）；`FollowUp(text)` + `Continue(ctx)`（user/toolResult 尾直接重试，assistant 尾先消费一条 steering 再消费一条 followUp，都没有则报错）；`Prompt`/`Continue` 运行守卫（`ErrRunInProgress`），`Steer`/`FollowUp` 可在 run 中并发调用
- [x] ctx 取消传播到每个在途工具与 LLM 请求；测试含 goroutine 泄漏检测（`runtime.NumGoroutine` 前后对比）
- [x] 测试：完成序 vs 源序（含并行加速断言）/ sequential 整批退化 / steering 边界注入且恰好一次 / FollowUp+Continue / assistant 尾无输入报错 / 并发 run 拒绝 / 取消后无泄漏

## 设计要点

- 事件完成序 ≠ 持久化序，这是并行模式的核心不变量，测试必须显式锁定。
- steering 队列只在轮次边界排空，不打断在途流式响应。

## 验收标准

- 并行 / 串行 / 混合三场景测试绿
- `go test -race ./agent/...` 无竞争、无泄漏

## 验证命令

```bash
go test -race ./agent/...
```

## 实施备注（2026-09-22）

- **`sync.WaitGroup` 替代 errgroup**：`golang.org/x/sync` 是外部模块，与 PLAN §15"零第三方运行时依赖"红线冲突；语义等价（批内错误不互相中断，各自落 isError 结果）。
- **`ExecutionMode` 做成可选接口**而非 `Tool` 必选方法：绝大多数工具默认并行，不该为默认值强制实现方法；与 phase 文档的字面写法略有出入，语义一致。
- **预检段语义对齐 pi**：全部 `ToolExecutionStart` 按源序先发；预检失败的调用（未知工具/非法参数/被 before-hook 拦截）当场 `ToolExecutionEnd`，不进执行段；`AfterToolCall` 只作用于真正执行过的结果。
- **Continue 语义**：user/toolResult 尾直接重试现有上下文（steering 若有则先注入）；assistant 尾无法直接续发，必须有一条排队输入（steering 优先于 followUp）。
- 时序敏感测试（120ms/10ms 完成序、100ms 串行退化、取消泄漏）以 `-count=3 -race` 复跑验证稳定性。
