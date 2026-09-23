# Phase 9：skills 与 memory（M4 上半）

> 里程碑：M4（第 1/2 步）｜ 前置：Phase 8 ｜ 状态：已完成（2026-09-23）

## 目标

skills（Agent Skills 标准）与 memory（长期记忆）两个子系统落地（PLAN §8 / §9）。

## 任务清单

### skills

- [x] `skills/skill.go`：`SKILL.md` frontmatter（name / description）解析——扁平 `key: value` 子集（引号剥离、注释/空行容忍、未知键忽略）；缺 name/description 报错但由发现层降级为警告
- [x] `skills/discover.go`：`Discover`（SKILL.md 递归 + 根层散装 .md、坏候选跳过并出 warning、后列目录同名覆盖前者）、`ProjectDirs`（cwd 向上到 git root 的 `.motionloop/skills` 与 `.agents/skills`）、`DefaultDirs`（全局 motionloop → 共享 `~/.agents/skills` → 受信任项目目录）、`Index`（`- name: description` 索引渲染）
- [x] 注入：cmd 将 `skills.Index` 写入 system prompt 的 `skills` 分区（Phase 7 占位接通）；无 skills 则分区留空跳过
- [x] `tools/skills_load.go`（位于 `tools/skills_memory.go`）：按需加载全文，未知名报错并列出可用清单
- [x] 测试：发现矩阵（全局/项目/共享/嵌套/散装/坏候选警告/同名覆盖）、frontmatter 九例容错表、ProjectDirs 到 git root、索引格式、**真实 `~/.agents/skills` 发现**（本机 25 个全部可读）

### memory

- [x] `memory/store.go`：`<root>/<workspace-slug>/` 一文件一事实（frontmatter name/description/type）+ `MEMORY.md` 索引（写时全量重建、按名排序、一行一条）；name 清洗（kebab 化，含非 ASCII）；类型校验（user/feedback/project/reference）；`Facts` 列举；`UsageSection` 站位说明（背景上下文而非指令 + memory_save 使用时机）
- [x] `tools/memory_save.go`：写事实 + 重建索引；同名更新；非法类型拒绝
- [x] 读取：cmd 将 `UsageSection + Index` 写入 `memory` 分区（Phase 7 占位接通）
- [x] 测试：save/更新去重/索引一致性、workspace 隔离、校验矩阵、name 清洗、空索引、以及**组装测试**（memory 保存 + skills 扫描 → sections 渲染含两个索引与背景上下文标注）

## 设计要点

- skills 是提示词层能力（不改工具集），memory 是跨会话事实层——两者都只通过 system prompt 的分区注入，不引入新的循环机制。
- memory 写入由系统提示词约定时机（用户偏好、纠正、项目约束），工具只负责存取与一致性。

## 验收标准

- 本机已有的一组 `~/.agents/skills` 被正确发现、索引可见、可加载
- memory 写入后新会话的提示词中出现索引条目

## 验证命令

```bash
go test ./skills/... ./memory/...
```

## 实施备注（2026-09-23）

- **skills_load 落 session 的方式**：phase 文档写"以 system patch 记入"，实现为普通 toolResult——加载内容经 toolResult 进入上下文并落 session，重放时自动恢复，再打 system patch 会造成重复注入。渐进披露语义不受影响。
- **同名 skills 覆盖顺序**：`DefaultDirs` 全局 → 共享 → 项目，发现层后列目录覆盖前者——项目级可以覆盖全局同名 skill。
- **散装根 .md**：pi 只在自己的目录（`~/.pi/agent/skills`、`.pi/skills`）认根层 .md；motionloop 对所有目录宽容接受（有合法 frontmatter 即认），更 lenient，实测对本机 `~/.agents/skills` 无副作用。
- **发现实现教训**：WalkDir 先访问目录再访问其文件，对含 SKILL.md 的目录 SkipDir 会把文件一并跳过——重写为"根层 ReadDir + 全量找 SKILL.md"两段式后才正确。
- **memory 索引重建**：每次 Save 后全量重建（读目录内全部事实文件重排序）——事实数量小，简单正确优先；MEMORY.md 与事实文件的一致性由重建保证。
- 两个新工具不进 `tools.Coding` preset（需要运行期状态：skills 列表、memory store + slug），由 cmd 在 profile 模式下追加装配；`--system-prompt` 整体替换模式下不装配（提示词被替换，索引无从谈起）。
