# motionloop

Go 实现的通用 Agent 框架（可自定义系统提示词 + 通用 tool 工具集 + 可扩展 LLM provider 插件）。核心 coding agent harness 设计参考 [pi](https://github.com/earendil-works/pi)（仅核心部分）。**框架优先**：`cmd/motionloop` 只是库的最大号使用者。

## 先读

- `PLAN.md` — 总体架构 + 全部已决策记录（§15），改架构前必读
- `docs/phase-0.md` … `phase-11.md` — 分阶段开发计划，当前进度看各文件头部的状态行
- 完成一个 Phase 时同步更新对应 phase 文档（状态 + checkbox + 实施备注）

## 布局与依赖方向

根级公开包，**不用 `pkg/` 前缀**（Q4 已决策）。依赖单向自上而下：

```
cmd → profile → (prompt, tools, skills, memory, session) → agent → llm
```

`agent` 与 `llm` 不依赖任何上层；`tools` 只依赖 `agent` 的 Tool 接口。

## 常用命令

```bash
go build ./... && go vet ./... && go test ./...   # 验收基线
go test ./llm/ -update                            # 重新生成 golden 文件（llm/testdata/）
go run ./cmd/motionloop --version                 # CLI（Phase 2 前仅占位，-p 未实现）
```

e2e 测试用 `MOTIONLOOP_E2E=1` + build tag `e2e` 才跑，CI 默认跳过。

## 已决策的架构红线（不要重新讨论，PLAN §15）

- **零第三方运行时依赖**：仅标准库；测试代码最多可用 testify
- **Session = JSONL**，entry schema 结构性采用 pi 的设计（首行 SessionHeader + type/id/parentId/timestamp 树 + system message 承载 sections 与 toolsAdded/toolsRemoved），不承诺字节级兼容
- `llm.Message` 及 content blocks 的 JSON 字段名与 pi 对齐（camelCase），**不要重命名字段**——session 落盘零转换依赖它
- 配置文件格式 JSON（settings.json / models.json）
- 不做子进程插件/MCP；不做 TUI（行式输出）
- 并行工具执行：事件按完成序发出，toolResult 按 assistant 源序持久化

## 代码约定

- 注释、commit message 英文；规划类文档（PLAN、phase docs）中文；README 英文 + `README.zh-CN.md` 双份同步
- 错误用 `fmt.Errorf("...: %w")` 包装，`errors.Is/As` 判定；业务错误不走 panic
- `context.Context` 一律为首参并贯穿取消传播；goroutine 归属明确（errgroup）
- 测试 table-driven、golden 文件锁 wire 格式；交付前 `gofmt -l .` 必须为空

## Git

- 提交只在 `develop` 分支，**严禁直接提交到 `main`**；合并回 main 走 develop
- 默认不主动 commit/push，完成后把改动留在工作区
