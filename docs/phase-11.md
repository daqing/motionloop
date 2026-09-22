# Phase 11：REPL、headless、文档与 CI（M5 / v0.1 发布）

> 里程碑：M5（**v0.1 发布**）｜ 前置：Phase 10 ｜ 状态：未开始

## 目标

交互体验、headless 模式、文档与 CI 收尾，发布 v0.1（PLAN §11 / §12 / §14）。

## 任务清单

- [ ] 交互 REPL：
  - readline 输入（纯 Go 轻量实现，不引 bubbletea——Q3 已决策 TUI 暂不规划）
  - 流式文本输出、工具执行实时展示（bash 命令 + 摘要输出、edit 的 diff）
  - 最小指令：`/exit`、`/model`、`/compact`、`/fork`
  - Ctrl+C 一次中断当前 run、两次退出（ctx 取消语义的 UI 呈现）
- [ ] `--headless`：stdout JSON 事件流（每行一个事件，字段对齐 agent 事件类型），供程序包装——这是 motionloop 作为"通用框架 + 可嵌入 CLI"的交付面
- [ ] 文档：
  - `README.md`（英文：定位、安装、5 分钟库使用示例、CLI 用法、自定义 provider / 提示词 / skills）+ `README.zh-CN.md` 同步对应
  - 公开包 `doc.go` 与 `example_test.go`（`go doc` 可读、示例可复制运行）
- [ ] CI（GitHub Actions）：`gofmt` 检查、`golangci-lint`（vet + staticcheck）、`go test -race ./...`；e2e 标签默认跳过（`MOTIONLOOP_E2E=1` 才跑）
- [ ] `LICENSE`（MIT）、`CHANGELOG.md` 起步
- [ ] 收尾核对：PLAN §12 M0–M5 验收清单逐项打勾，打 `v0.1` tag

## 设计要点

- REPL 与 headless 消费同一套事件流——事件系统（Phase 2/3）是唯一 UI 契约，REPL 只是其中一种渲染器。
- README 的第一个示例必须是"作为 Go 库组装一个最小 agent"（框架优先的定位，PLAN §1）。

## 验收标准（M5 / v0.1，PLAN §12）

- CI 全绿
- README 双语齐且示例可复制运行
- 发布 `v0.1` tag

## 验证命令

```bash
test -z "$(gofmt -l .)" && go vet ./... && go test -race ./... && golangci-lint run
go run ./cmd/motionloop            # REPL
echo "hi" | go run ./cmd/motionloop --headless -p "hi"
```
