# Phase 0：仓库骨架与 `llm` 消息模型

> 里程碑：M0（第 1/3 步）｜ 前置：无 ｜ 状态：未开始

## 目标

搭好仓库骨架，落地 `llm` 包的全部核心类型：消息模型、内容块、用量、流事件、模型元数据。这些类型是 provider、agent、session 三方的公共语言，必须最先稳定（PLAN §4.1）。

## 任务清单

- [ ] 删除根 `main.go`，新建 `cmd/motionloop/main.go`（暂时只打印版本占位）
- [ ] `.gitignore`（`motionloop` 二进制、`dist/`、`.DS_Store`）
- [ ] `llm/doc.go`：包说明（对标 pi-ai 的定位）
- [ ] `llm/message.go`：
  - `type Role string`：`system` / `user` / `assistant` / `toolResult`
  - `Message`：Role、`Content []ContentBlock`、`ToolCallID`（toolResult 专用）、Timestamp
  - `ContentBlock` 接口 + 四种实现：`TextBlock`、`ImageBlock`（data/mimeType）、`ToolCallBlock`（id/name/arguments）、`ThinkingBlock`（thinking/signature）
- [ ] `llm/usage.go`：`Usage`（input/output/cacheRead/cacheWrite/reasoning/totalTokens + Cost）
- [ ] `llm/model.go`：`Model`（ProviderID、ModelID、ContextWindow、MaxOutput、能力标志）
- [ ] `llm/stream.go`：`StreamEvent` 变体（TextDelta / ThinkingDelta / ToolCallDelta / Usage / Stop{Reason, ErrorMessage}）；`StopReason` 枚举：`stop` / `length` / `toolUse` / `error` / `aborted`
- [ ] JSON round-trip golden 测试（`llm/testdata/`）

## 设计要点

- content block 的 JSON 字段名对齐 pi session-format（`type/text/data/mimeType/id/name/arguments`），后续 session 落盘零转换（PLAN §7）。
- `StopReason` 取值对齐 pi；`pending` 只存在于流式过程，永不落盘。
- 零第三方依赖（PLAN §14）。

## 验收标准

- `go build ./...` 与 `go vet ./...` 通过
- `llm` golden round-trip 测试通过
- `cmd/motionloop` 可运行

## 验证命令

```bash
go build ./... && go vet ./... && go test ./...
go run ./cmd/motionloop --version
```
