# Phase 5：session JSONL——pi entry schema（M1 收口）

> 里程碑：M1（**M1 验收在本阶段完成**）｜ 前置：Phase 4 ｜ 状态：已完成（2026-09-22）

## 目标

会话持久化落地：pi 式 entry schema（首行 header / 信封树结构 / 重放式状态重建），写、重放、resume、fork（PLAN §7）。

## 任务清单

- [x] `session/entry.go`：`Header{type:"session", version, id, timestamp, cwd, parentSession}`；`Entry` 信封（`type/id/parentId/timestamp` + 按 type 选择的 payload）；entry 类型 `message` / `model_change` / `thinking_level_change` / `usage`；8-hex entry id 与 UUIDv4 session id
- [x] `session/format.go`：JSONL 解析（撕裂末行丢弃并报告 `TornTrailing`，中部坏行判为损坏报错）；`migrate` 版本迁移钩子（v1 起步）；`chainOf` 树遍历（末条 entry 起回溯，支持跨文件 fork 点）；`foldState` 重放（message 入列 + system message 的 `sections` patch（空值删除）与 `toolsAdded`/`toolsRemoved` fold 出当前提示词与工具集）
- [x] `session/manager.go`：`Manager.Create/Load`（Load 重放后重新以追加模式打开 = resume）；`Session.AppendMessage/AppendModelChange/AppendThinkingLevelChange/AppendUsage`（O(1) 追加、自动挂 tip）；`Branch(entryID)` 原地分支（改 tip 指针，落盘于下一条 append）；`Fork()` 新文件 + `parentSession` 回链（子文件首条 entry 的 parentId 指回父文件分叉点）；路径 `<root>/<workspace-slug>/<timestamp>_<id>.jsonl`、`DefaultRoot` = `~/.motionloop/sessions`
- [x] `session/recorder.go`：`Recorder` 订阅 agent 事件，`MessageEnd` 逐事件追加落盘（WAL）；写失败即停并经 `Err()` 上报
- [x] 测试：golden 锁 wire 格式（含全部 entry 类型 + 解析回读）/ 撕裂末行丢弃且 tip 正确 / 中部损坏拒绝 / sections+tools+model+thinkingLevel+usage fold / fork 后两支线独立演进（B 不含 A 的 fork 后消息）/ 原地 branch / slug 规则 / **kill-resume 集成**（录制→关闭→Load→重建 agent 续聊→全文件重放恰好一次）

## 设计要点

- 结构性采用 pi schema、不做字节级兼容承诺（PLAN §15 Q2）；motionloop 特有事件一律新增 `type`，不改信封。
- 重放语义：load = 纯函数（文件 → Agent 状态），这是 resume、fork、导出、compaction 共同的地基。

## 验收标准（M1，PLAN §12）

- kill 进程后 `resume` 恢复对话
- fork 分支后两条支线独立演进

## 验证命令

```bash
go test -race ./session/... ./agent/...
```

## 实施备注（2026-09-22）

- **fork 的跨文件链解析**：fork 文件只有 header + `parentSession` 回链，其首条 entry 的 `parentId` 指回**父文件内**的分叉点 id；`Load` 遍历到本文件解析不了的 parentId 时递归进父文件（`loadUpTo`），因此父会话后续演进不会泄漏进子链——两支线天然独立。零条目 fork 文件载入父链当前状态，Session 续写自动挂父链 tip。
- **section 删除语义**：pi 的 `sections` patch 用 null 删除；我们的 `map[string]string` 用**空串值**表示删除（applySystemPatch 中 delete），已文档化。
- **原地 Branch 是内存 tip 指针**：文件不动，回退在下一条 append 落盘时才可见——与 append-only WAL 一致。
- 解析目前整文件读入（会话文件 MB 级可接受）；撕裂末行丢弃并以 `State.TornTrailing` 报告，中部坏行视为损坏直接报错。
- `usage` entry 记录最近一次汇总（`State.Usage` 取最后一条）；累积统计留给后续 UI/账单需求。
- golden（`session/testdata/session.jsonl`，`go test ./session/ -update` 再生成）与 pi session-format 逐字段对齐：`toolCallId`/`thinkingSignature`/`sections`/`stopReason` 等 camelCase 结构。
