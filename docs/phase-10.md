# Phase 10：compaction 与 session CLI（M4 收口）

> 里程碑：M4（第 2/2 步，**M4 验收在本阶段完成**）｜ 前置：Phase 9 ｜ 状态：已完成（2026-09-23）

## 目标

上下文 compaction 与 session 管理子命令（PLAN §7）。

## 任务清单

- [x] `session/compact.go`：
  - `TokenEstimator`（ASCII ~4 字符/token、CJK ~1.5 rune/token，系数可调）
  - `Compactor.Transform`（`agent.TransformContext` 实现）：视图超阈值时，把"已摘要之外、保留尾巴之前"的老化消息交 provider 摘要（指令要求保留任务目标/关键决策/未完成事项/重要路径），追加 compaction entry，返回压缩视图；run 内 memo 避免重复摘要；摘要失败降级为未压缩视图
  - 摘要模型可配（`SummaryModel`，默认当前模型）；触发阈值可配（settings 的 `compactionThreshold`，0 → 默认 24000，负值禁用）
- [x] `compactedView` **同一函数服务两侧**：live 转换（TransformContext 产物）与重放折叠（`applyEntry` 的 compaction 分支）共享，保证"重放视图 == 当时的请求视图"；`summarizedCount` 相对**当前折叠态**的非系统消息计数，transcript 与折叠态在任意时刻相等 → 两边恒等；system 消息（含 patch）永不摘要
- [x] compaction entry 入 session（新增 `type`，信封不动）；原始行永不删除
- [x] cmd 重构：抽取 `prepare`/`buildOptions` 共享装配；`-p` 模式现在**创建并录制 session**、挂 Compactor；`session resume` 复用装配（重放种子 + 续录 + 录制的 model_change 优先于未指定的 flag）
- [x] `cmd/motionloop` 子命令：`session ls`（时间 + 短 id + fork 标记 + 首条 user 摘要）、`session show <id>`（全部 entry + 当前链标记 `*` + compaction 概览）、`session fork <id>`（新文件 + parentSession 回链）、`session resume <id> -p <prompt>`（id 前缀唯一匹配）
- [x] 测试：compactedView（含中途 system patch 存活）/ 估算器 ASCII 与 CJK / 重放一致性（fold == 期望视图）/ 全链路（摘要恰一次、请求视图含摘要缺老化消息、文件保留全部原始行、重放视图 == 请求视图）/ 阈值下不触发 / 负值禁用；config 合并新增 `compactionThreshold` 表驱动（global→project 分层、负值禁用、非整数拒绝）；CLI 冒烟（真实二进制：ls/show/resume/fork 四子命令 + 无 key 时 -p 与 resume 仍走完整装配并落 session）

## 设计要点

- compaction 只动 transcript 视图（`TransformContext` 的产物），**不销毁原始 session 行**——文件是只追加的事实记录，压缩是重放时的视图变换。
- 摘要提示词要求保留：任务目标、关键决策、未完成事项、重要文件路径。

## 验收标准（M4，PLAN §12）

- 长会话 compaction 后继续对话，不失关键上下文
- session 子命令可用

## 验证命令

```bash
go test ./session/...
go run ./cmd/motionloop session ls
```

## 实施备注（2026-09-23）

- **一致性不变量**：agent transcript 与 session 折叠态在任意时刻相等（resume 用 `state.Messages` 种子、录制同步追加），compaction 的 `summarizedCount` 相对当前折叠态计数，`compactedView` 同一函数两侧共用——因此"重放视图 == 当时请求视图"是构造保证而非巧合（测试 DeepEqual 锁定）。
- **摘要消息是 system 角色**（无 sections，不参与提示词折叠），插在 leading system 块之后；中途的 system patch 永不被摘要。
- **run 内 memo**：Compactor 记住本次 run 已摘要的 count/summary，TransformContext 每次请求重入时先复用旧摘要构造视图，只有新消息把视图顶过阈值才再次摘要（测试断言 provider 恰被调用一次做摘要）。
- **阈值配置**：settings 的 `compactionThreshold`（估算 token；0 = 默认 24000，负值禁用）；`SummaryModel` 字段预留摘要模型覆盖，配置面后续按需补。
- **-p 模式开始录制 session**：此前 CLI 一次性运行不落盘；现在每次 `-p` 创建 session 并录制（provider 失败也会保留 user 消息行——冒烟验证）。`session resume` 优先采用会话内记录的 model_change。
- 冒烟发现的真实数据顺便验证了 skills 容错：本机 `~/.agents/skills/signing-in-to-aws` 的 frontmatter 非法，被降级为警告而非中断。
- **resume 冒烟暴露并修复一个 agent 缺陷**：loop 的首轮流事件原先只要 `systemMsg` 存在就发射——resume 场景 transcript 已有种子，system 消息并未真正追加，事件却让 recorder 落了**重复的 system 行**。现以 `freshSystem` 标记：仅当 Prompt 真正追加时才发射（`TestResumeDoesNotDuplicateSystemMessage` 锁定）。
- **resume 的 provider/model 语义**（对齐 pi）：会话头不记录初始模型，只有 `model_change` 条目参与恢复——resume 时录制的 model_change 优先于未指定的 flag，否则回落 flags/settings；跨 provider 恢复需显式 `--provider`。
