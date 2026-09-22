# Phase 0：仓库骨架与 `llm` 消息模型

> 里程碑：M0（第 1/3 步）｜ 前置：无 ｜ 状态：已完成（2026-09-22）

## 目标

搭好仓库骨架，落地 `llm` 包的全部核心类型：消息模型、内容块、用量、流事件、模型元数据。这些类型是 provider、agent、session 三方的公共语言，必须最先稳定（PLAN §4.1）。

## 任务清单

- [x] 删除根 `main.go`，新建 `cmd/motionloop/main.go`（暂时只打印版本占位）
- [x] `.gitignore`（`motionloop` 二进制、`dist/`、`.DS_Store`、`*.test`、`coverage.out`）
- [x] `llm/doc.go`：包说明（对标 pi-ai 的定位）
- [x] `llm/message.go`：
  - `type Role string`：`system` / `user` / `assistant` / `toolResult`
  - `Message`：Role、`Content []ContentBlock`、`ToolCallID`（toolResult 专用）、Timestamp
  - `ContentBlock` 接口 + 四种实现：`TextBlock`、`ImageBlock`（data/mimeType）、`ToolCallBlock`（id/name/arguments）、`ThinkingBlock`（thinking/signature）
- [x] `llm/usage.go`：`Usage`（input/output/cacheRead/cacheWrite/reasoning/totalTokens + Cost）
- [x] `llm/model.go`：`Model`（ProviderID、ModelID、ContextWindow、MaxOutput、能力标志）
- [x] `llm/stream.go`：`StreamEvent` 变体（TextDelta / ThinkingDelta / ToolCallDelta / Usage / Stop{Reason, ErrorMessage}）；`StopReason` 枚举：`stop` / `length` / `toolUse` / `error` / `aborted`
- [x] JSON round-trip golden 测试（`llm/testdata/`）

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

## 实施备注（2026-09-22）

- `Message` 在任务清单核心字段之外，一并落了 pi 对齐的完整消息面：toolResult 的 `toolName`/`isError`、system 的 `sections` + `toolsAdded`/`toolsRemoved`（`ToolDecl`）、assistant 的 `provider`/`model`/`responseModel`/`thinkingLevel`/`stopReason`/`usage`/`errorMessage`。依据：PLAN §15 Q2（entry schema 采用 pi）＋ 本 Phase "类型是三方公共语言、必须最先稳定" 的目标，避免 Phase 2/5 再动这块。
- content 反序列化兼容 `string | blocks` 两种形态（pi 的宽松格式），序列化恒为数组。
- golden 测试支持 `go test ./llm/ -update` 重新生成；round-trip 比较前会对 `arguments`/`parameters` 内嵌 JSON 做 compact 归一化，避免格式差异误报。
- 零第三方依赖：全部 import 为标准库，`go.mod` 无 require。
