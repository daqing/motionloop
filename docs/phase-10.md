# Phase 10：compaction 与 session CLI（M4 收口）

> 里程碑：M4（第 2/2 步，**M4 验收在本阶段完成**）｜ 前置：Phase 9 ｜ 状态：未开始

## 目标

上下文 compaction 与 session 管理子命令（PLAN §7）。

## 任务清单

- [ ] `session/compact.go`：
  - token 估算（字符数近似，per-language 系数可调）
  - 超阈值触发：旧消息交摘要模型压缩为一条 summary system message（`role: system` 的 compaction entry，对齐 pi 的 `compactionSummary` 思路），保留最近 N 轮原文
  - 摘要模型可配（默认当前模型）；触发阈值可配（settings）
- [ ] 接入 `TransformContext` hook：每次请求前检查并按需压缩（PLAN §4.2 的管道设计）
- [ ] compaction 事件入 session（新增 `type`，不改信封），重放后上下文即压缩态
- [ ] `cmd/motionloop` 子命令：`session ls`（列表 + 时间 + 摘要）、`session show <id>`（渲染消息树）、`session resume <id>`、`session fork <id>`
- [ ] 测试：超长会话触发 compaction 后上下文显著变短且对话可继续；压缩后的 session 重放一致；`ls` / `show` / `resume` / `fork` happy path

## 设计要点

- compaction 只动 transcript 视图（`TransformContext` 的产物），**不销毁原始 session 行**——文件是只追加的事实记录，压缩是重放时的视图变换。
- 摘要提示词要求保留：任务目标、关键决策、未完成事项、重要文件路径。

## 验收标准（M4，PLAN §12）

- 长会话 compaction 后继续对话，不失关键上下文
- session 子命令可用

## 验证命令

```bash
go test ./session/...
go run ./cmd/motionloop session ls
```
