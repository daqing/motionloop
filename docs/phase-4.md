# Phase 4：并行工具执行与 steering

> 里程碑：M1（第 2/4 步）｜ 前置：Phase 3 ｜ 状态：未开始

## 目标

工具批默认并行执行 + per-tool 串行覆盖 + 运行中途插入用户消息（steering / followUp）。

## 任务清单

- [ ] `agent/loop.go`：`errgroup` 并行执行同批 tool calls；`tool_execution_end` 按**完成序**发出，toolResult 消息按 **assistant 源序**入 transcript（对齐 pi 的 parallel 语义，PLAN §2）
- [ ] `Tool.ExecutionMode()`：`Sequential` 覆盖（write/edit 这类）；批内任一 sequential → 整批退化为串行
- [ ] steering：`Agent.Steer(msg)` 入队，轮次边界注入（one-at-a-time 模式）；`FollowUp(msg)` + `Continue()`
- [ ] ctx 取消传播：在途工具、在途 LLM 请求全部收到取消；无 goroutine 泄漏
- [ ] 测试：
  - 并行批乱序完成，但持久化 toolResult 有序
  - 混合批（含 sequential 工具）整体串行
  - steering 消息在下一轮生效、注入一次即出队
  - 取消后全部 goroutine 退出（泄漏检测：`runtime.NumGoroutine` 前后对比）

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
