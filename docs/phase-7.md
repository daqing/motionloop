# Phase 7：系统提示词体系与 coding profile（M2 收口）

> 里程碑：M2（第 2/2 步，**M2 验收在本阶段完成**）｜ 前置：Phase 6 ｜ 状态：已完成（2026-09-22，真实端点 e2e 待用户带 key 复验）

## 目标

`prompt` 包（分区模板 + 动态注入）与 `profile/coding`（完整 coding agent 提示词）落地，"可自定义系统提示词"从框架能力变成产品事实（PLAN §6）。

## 任务清单

- [x] `prompt/template.go`：`Sections`（canonical 顺序 identity/environment/tools/skills/memory/rules + 追加自定义）；`Patch`（按名替换、空值删除——与 session 折叠同语义）；`ToMessage`（content 供 provider、sections 供重放）；`ApplyOverrideDir`（`<section>.md` 覆盖，缺目录 no-op）
- [x] `prompt/builder.go`：`DetectEnvironment`（cwd、平台/架构、日期、git 分支与 dirty 探测，无 git/非仓库时优雅降级）+ `EnvironmentSection` 渲染；skills/memory 分区由 sections 机制承载，Phase 9 注入内容
- [x] `profile/profile.go`：`Profile{Name, Prompt, Tools, ThinkingLevel}` + `AgentOptions` 一键转 agent options
- [x] `profile/coding/`：完整系统提示词（identity 身份与验证文化、environment 动态事实、tools 七工具使用规范、rules git 约定与安全边界），参考 pi 的 system-prompt 设计
- [x] 三级覆盖链：内置 → `~/.motionloop/prompts/<profile>/` → `.motionloop/prompts/<profile>/`（项目级 trust 留 TODO 到 Phase 8）；CLI `--system-prompt <file>` 整体替换、`--profile` 选择
- [x] mid-session 变更：`agent.PatchPrompt(sections)` 与 `agent.SetTools(...)`（带 `toolsAdded`/`toolsRemoved` diff 消息、无变化不发消息、run 中拒绝）→ 经 recorder 落 session，重放折叠验证
- [x] e2e（脚本化替代）：`profile/coding` 全组装的 agent 在真实临时 workspace 完成 read → edit → bash 验证 → 汇报（真实工具全链路；远程端点版待 key）
- [x] 测试：sections 顺序/渲染/patch/消息、覆盖链 global→project、git 探测（init/commit/dirty 三态）、`WithSystemMessage`+patch 落 session 重放、SetTools diff/声明/移除/no-op、coding profile 形状与 e2e

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

## 实施备注（2026-09-22）

- **e2e 暴露并修复了一个真实缺陷**：`Bash` 原先没有默认工作目录——edit 在 workspace 内成功、`bash cat` 却跑在进程 CWD。现在 `Bash{Dir}` 可设默认目录，`Coding(ws)`/`Minimal(ws)` 装配 `Bash{Dir: ws.Root}`（工具参数 `workdir` 仍可覆盖）。bash 依旧不做路径限制，`Dir` 只是默认值。
- **prompt.md 模板改为 Go consts**：分区由多段文本 + 环境事实动态拼装，单一模板文件表达不了；覆盖定制走覆盖链（`<section>.md`），不需要改内置文本的编辑性。
- **系统提示词即 transcript 状态**：初始 system message 同时携带渲染文本（provider 直接用）与 sections（重放折叠）；`PatchPrompt`/`SetTools` 的 diff 消息经 recorder 落 session。注意：openai 兼容端点接受多条 system 消息；anthropic provider（Phase 8）需在转换层合并——已在其任务范围。
- `SetTools` 无 diff 不发消息（避免噪音）；`PatchPrompt`/`SetTools` 复用 run 守卫，run 进行中拒绝。
- 覆盖链的 trust 空缺已留 TODO（Phase 8 `config/trust.go` 统一处理）；`--system-prompt` 是整体替换、绕过 sections（一次性场景）。
- 真实端点 e2e 未跑（无 key）；`profile/coding` 集成测试覆盖了除远程端点外的全链路，用户可用：
  `GLM_API_KEY=... MOTIONLOOP_E2E=1 go run ./cmd/motionloop --provider glm -p "..."` 复验。
