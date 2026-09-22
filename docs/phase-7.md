# Phase 7：系统提示词体系与 coding profile（M2 收口）

> 里程碑：M2（第 2/2 步，**M2 验收在本阶段完成**）｜ 前置：Phase 6 ｜ 状态：未开始

## 目标

`prompt` 包（分区模板 + 动态注入）与 `profile/coding`（完整 coding agent 提示词）落地，"可自定义系统提示词"从框架能力变成产品事实（PLAN §6）。

## 任务清单

- [ ] `prompt/template.go`：命名 sections 组装（`identity` / `environment` / `tools` / `skills` / `memory` / `rules`）；增量 patch（`sections` 按名替换、null 删除）——与 session 的 system message 格式直接打通
- [ ] `prompt/builder.go`：动态注入环境事实——cwd、平台、日期、git 状态探测（分支/是否有未提交变更）、skills 索引占位（Phase 9 接入）、memory 索引占位（Phase 9 接入）
- [ ] `profile/profile.go`：`Profile` 概念 = prompt 模板 + 工具 preset + 默认 thinking level
- [ ] `profile/coding/`：完整系统提示词（`prompt.md` 模板 + Go 装配代码），内容设计参考 pi 的 `system-prompt.ts`——身份、环境、工具使用规范、git 约定、安全边界
- [ ] 三级覆盖链：内置默认 → `~/.motionloop/prompts/` → `.motionloop/prompts/`（项目级需 trust，Phase 8 实现 trust 本阶段先留 TODO）；CLI `--system-prompt <file>` 整体替换
- [ ] mid-session 提示词 / 工具集变更 → system patch 消息入 session（重放验证，衔接 Phase 5）
- [ ] e2e：coding profile 完成真实小任务（读代码 → edit 修改 → bash 跑测试 → 汇报）

## 设计要点

- 系统提示词即 transcript 状态：分区 patch 落在 system message 里，重放即重建（Phase 5 已铺好的机制），不另设提示词状态文件。
- 动态注入在会话启动时求值一次；跨天长会话的日期漂移暂不处理（记入已知限制）。

## 验收标准（M2，PLAN §12）

- e2e 真实任务通过
- 三级覆盖与 `--system-prompt` 生效
- 提示词 patch 后 session 重放状态正确

## 验证命令

```bash
go test ./prompt/... ./profile/...
MOTIONLOOP_E2E=1 go run ./cmd/motionloop -p "给 tools/read.go 加一个 xxx 参数并跑测试"
```
