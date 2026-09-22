# Phase 1：openaicompat provider 与注册表

> 里程碑：M0（第 2/3 步）｜ 前置：Phase 0 ｜ 状态：未开始

## 目标

实现第一个 LLM provider：OpenAI-compatible（chat completions + SSE 流式 + tool calls），外加 provider 注册表。一个实现覆盖 OpenAI/DeepSeek/GLM/Kimi/Ollama/vLLM 等一切兼容端点（PLAN §4.1）。

## 任务清单

- [ ] `llm/provider.go`：`Provider` 接口（`ID()`、`Stream(ctx, model, messages, opts) (<-chan StreamEvent, error)`、`Capabilities(Model)`）+ 全局 `Register` / `Resolve`
- [ ] `llm/options.go`：`StreamOptions`（APIKey、BaseURL、MaxTokens、Temperature、ThinkingLevel、Timeout、Tools）
- [ ] `llm/provider/openaicompat/provider.go`：
  - 请求组装：`llm.Message` → chat completions body；Tool schema → `tools` 字段
  - SSE 解析：`data:` 行、`[DONE]` 结束符；delta 累积为 TextDelta / ThinkingDelta / ToolCallDelta（按 index 拼接分片的 arguments JSON）
  - usage 提取（`stream_options.include_usage`）
  - 错误语义：HTTP 4xx/5xx、流中断 → 事件流中的 `Stop{Reason: error}`，**不向调用方抛 error**
- [ ] `llm/provider/openaicompat/models.go`：内置常用模型条目（gpt / deepseek / glm 各若干）
- [ ] 测试：`httptest` 假服务器回放 SSE 报文，五类场景各一组——纯文本、tool call 分片、usage、HTTP 错误、流中断；golden 断言 StreamEvent 序列

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
