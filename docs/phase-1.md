# Phase 1：openaicompat provider 与注册表

> 里程碑：M0（第 2/3 步）｜ 前置：Phase 0 ｜ 状态：已完成（2026-09-22）

## 目标

实现第一个 LLM provider：OpenAI-compatible（chat completions + SSE 流式 + tool calls），外加 provider 注册表。一个实现覆盖 OpenAI/DeepSeek/GLM/Kimi/Ollama/vLLM 等一切兼容端点（PLAN §4.1）。

## 任务清单

- [x] `llm/provider.go`：`Provider` 接口（`ID()`、`Stream(ctx, model, messages, opts) (<-chan StreamEvent, error)`、`Capabilities(Model)`）+ 全局 `Register` / `Resolve`（另附 `ProviderIDs()`）
- [x] `llm/options.go`：`StreamOptions`（APIKey、BaseURL、MaxTokens、Temperature、ThinkingLevel、Timeout、Tools）+ `ThinkingLevel` 分档常量
- [x] `llm/provider/openaicompat/provider.go` + `wire.go`（wire 类型与消息转换拆分）：
  - 请求组装：`llm.Message` → chat completions body；ToolDecl → `tools` 字段
  - SSE 解析：`data:` 行、`[DONE]`；delta 累积为 TextDelta / ThinkingDelta / ToolCallDelta（按 index 拼接分片的 arguments JSON）
  - usage 提取（`stream_options.include_usage`，含 cached/reasoning 明细映射）
  - 错误语义：HTTP 4xx/5xx、流中断、坏 chunk → `Stop{error}`，不向调用方抛 error
- [x] `llm/provider/openaicompat/models.go`：内置三个 endpoint 身份（openai / deepseek / glm，各两个模型条目），init 注册
- [x] 测试：`httptest` 假服务器回放 SSE 报文——五场景（纯文本含 thinking、tool call 分片、usage、HTTP 错误、流中断）+ 请求形状/消息转换 + ctx 取消（前后两个时机）与 goroutine 泄漏检查 + 注册表测试

## 设计要点

- Stream 契约（PLAN §4.1）：除 ctx 取消外不返回 error，失败编码为事件流末尾的 `Stop{error}`。
- ctx 取消必须关闭 HTTP body 与 channel，不泄漏 goroutine（`-race` 验证）。
- BaseURL 可整体覆盖（env / 后续 models.json），这是"一个 provider 通吃兼容端点"的关键。

## 验收标准

- fake server 五场景测试全绿
- （可选，需真实 key）`MOTIONLOOP_E2E=1 go test -tags e2e ./llm/provider/openaicompat/` 打真端点跑通一轮对话

## 验证命令

```bash
go test -race ./llm/...
```

## 实施备注（2026-09-22）

- **流完整性语义**：测试暴露了一个真实缺陷——body 干净 EOF 但未收到 `[DONE]` 时原实现会静默当作正常结束。现改为 `sawDone` 追踪：缺 `[DONE]` 即 `Stop{error, "stream interrupted"}`。连接被粗暴掐断则表现为 scanner 的 `unexpected EOF`，同样归入 interrupted。
- 一个代码路径多身份：`New(providerID, baseURL, models)` 绑定 endpoint 身份，内置注册 openai / deepseek / glm；自定义端点后续经 `models.json`（Phase 8）复用同一路径。
- APIKey 为空时**不发** Authorization 头——本地 Ollama 等免 key 端点的正确行为。
- thinking 兼容两种字段名：DeepSeek 系 `reasoning_content` 与 `reasoning`，都映射为 ThinkingDelta；请求侧由 ThinkingLevel 映射为 `reasoning_effort`（low/medium/high）。
- 消息转换：user 纯文本时 content 为字符串，含图片时为 parts 数组（data URI）；assistant 纯 tool call 时省略 content；thinking 块不回传请求侧。
- 真实端点 e2e（可选项）未跑（无 key）；留待 Phase 2 接上 CLI 后统一验证。
