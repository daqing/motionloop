# Phase 5：session JSONL——pi entry schema（M1 收口）

> 里程碑：M1（第 3/4 步）｜ 前置：Phase 4 ｜ 状态：未开始

## 目标

会话持久化落地：pi 式 entry schema（首行 header / 信封树结构 / 重放式状态重建），写、重放、resume、fork（PLAN §7）。

## 任务清单

- [ ] `session/entry.go`：
  - `SessionHeader{Type: "session", Version, ID, Cwd, ParentSession}`
  - 信封：除 header 外每行 `Type` / `ID` / `ParentID` / `Timestamp`
  - entry 类型：`message`（role + content blocks，system message 携带 `sections` 与 `toolsAdded`/`toolsRemoved`）、`model_change`、`thinking_level_change`、`usage`
- [ ] `session/format.go`：JSONL 编解码；**逐事件追加落盘**（WAL 风格，每条 message/turn 事件即 append）；崩溃撕裂末行检测与丢弃；`Version` 字段 + 加载迁移钩子（从 v1 起预留）
- [ ] `session/manager.go`：
  - `Append(entry)` O(1) 追加
  - `Load(path)`：逐行重放，按 `id`/`parentId` 建树，取当前叶子链重建 messages；重放 system message 的 `sections` patch 与 `toolsAdded`/`toolsRemoved` 得到当前提示词与工具集（**无独立状态文件**）
  - `Fork`：新 entry 挂到指定节点实现原地分支；新文件带 `parentSession` 回链
  - 存储路径：`~/.motionloop/sessions/<workspace-slug>/<timestamp>_<id>.jsonl`
- [ ] agent 集成：loop 事件 → session append；`resume` 从重放状态恢复 Agent 再继续
- [ ] 测试：
  - golden 文件锁 entry 格式
  - 撕裂末行（手写截断 JSON）→ load 丢弃且状态可用
  - fork 后两支线独立演进，各自重放的提示词与工具状态正确
  - 中途 `model_change` 后 resume，模型状态正确

## 设计要点

- 结构性采用 pi schema、不做字节级兼容承诺（PLAN §15 Q2）；motionloop 特有事件一律新增 `type`，不改信封。
- 重放语义：load = 纯函数（文件 → Agent 状态），这是 resume、fork、导出、compaction 共同的地基。

## 验收标准（M1，PLAN §12）

- kill 进程后 `resume` 恢复对话
- fork 分支后两条支线独立演进

## 验证命令

```bash
go test ./session/... ./agent/...
```
