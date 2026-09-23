# Phase 11：REPL、headless、文档与 CI（M5 / v0.1 发布）

> 里程碑：M5（**v0.1 发布**）｜ 前置：Phase 10 ｜ 状态：已完成（2026-09-23，v0.1.0 tag 随本提交创建）

## 目标

交互体验、headless 模式、文档与 CI 收尾，发布 v0.1（PLAN §11 / §12 / §14）。

## 任务清单

- [x] 交互 REPL（`cmd/motionloop/repl.go`）：
  - 行式输入（bufio + `» ` 提示符，纯标准库）
  - 复用 renderEvent 流式渲染（文本 stdout、工具活动 stderr、usage 汇总）
  - 指令：`/exit` `/quit` `/help` `/model [provider/]model`（agent 新增 `SetProvider/SetModel/SetStreamOptions` + `AppendModelChange` 落盘）、`/compact`（Compactor 新增 `Force`）、`/fork`
  - Ctrl+C：run 中→取消当前 run 并等待收尾，空闲→提示后第二次退出；run 中的输入进 `Steer()`（Phase 4 能力首次产品化）
- [x] `--headless`（`cmd/motionloop/headless.go`）：stdout 每行一个 JSON 事件（type/role/text/delta/tool/…），与 REPL 共用同一事件流——只是另一种渲染器
- [x] 文档：
  - `README.md` 重写（定位、安装、**5 分钟库使用示例**（框架优先）、CLI 全量用法、配置四件套 settings/models.json/prompts/skills+memory、包结构、状态）+ `README.zh-CN.md` 逐节对应
  - `agent/example_test.go`（Output 验证的最小 agent + 自定义工具）、`session/example_test.go`（录制→重放，编译型）；全部公开包均有包注释
- [x] CI（`.github/workflows/ci.yml` + `.golangci.yml` v2 配置）：gofmt 检查、`go vet`、golangci-lint（默认集：errcheck/staticcheck/unused/ineffassign…）、`go test -race ./...`；**本地预跑全绿**
- [x] `LICENSE`（MIT）、`CHANGELOG.md`（v0.1.0 全量条目）
- [x] 收尾核对：PLAN §12 M0–M5 逐项打 ✅（含各阶段验收证据索引）；版本号 `0.1.0-dev` → `0.1.0`；`v0.1.0` tag 随提交打上

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
printf "/help\n/exit\n" | go run ./cmd/motionloop
echo "hi" | go run ./cmd/motionloop --headless -p "hi"
```

## 实施备注（2026-09-23）

- **冒烟证据**：REPL 管道（`/help`+`/exit`、无 key 提示词→run 失败→继续循环→`/exit`）与 headless（JSON 事件流含真实 skills 索引注入、error stopReason、agent_end）均以真实二进制验证。
- **steering 首次产品化**：REPL 在 run 进行中把输入交给 `Steer()`——Phase 4 的队列能力第一次有了用户入口。
- **`/model` 联动**：切换即 `SetProvider+SetModel+SetStreamOptions`（重解析 key）并落 `model_change`，resume 按记录恢复。
- **golangci-lint v2 配置坑**：`issues.exclude-rules` 在 v2 移到 `linters.exclusions.rules`；测试代码的 errcheck 豁免由此配置（临时文件 Close/Write 忽略是测试惯例）。修掉的非测试问题：两处 `defer Body.Close` 显式忽略、edit.go 无效赋值、两处未使用函数删除。
- **lint 修复顺带删除**：`Workspace.rel` 与 `sortedSectionKeys`（未使用）；session 示例采用编译型（无 Output 注释不执行），避免示例测试产生文件写入。
- CI 在 GitHub Actions 首跑前无法远程验证，但四个门（gofmt/vet/lint/race test）均已在本地以相同工具链跑绿。
