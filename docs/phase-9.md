# Phase 9：skills 与 memory（M4 上半）

> 里程碑：M4（第 1/2 步）｜ 前置：Phase 8 ｜ 状态：未开始

## 目标

skills（Agent Skills 标准）与 memory（长期记忆）两个子系统落地（PLAN §8 / §9）。

## 任务清单

### skills

- [ ] `skills/skill.go`：`SKILL.md` frontmatter（name / description）解析，轻量容错（对齐 pi 的 lenient 策略：警告不致命）
- [ ] `skills/discover.go`：路径扫描——`~/.motionloop/skills/`、`~/.agents/skills/`（跨 harness 共享）、项目 `.motionloop/skills/` 与 `.agents/skills/`（向上到 git root，需 trust）
- [ ] 注入：name + description 索引进 system prompt 的 `skills` 分区（接 Phase 7 的占位）
- [ ] `tools/skills_load.go`：按需加载 SKILL.md 全文（渐进披露；加载后以 system patch 记入 session）
- [ ] 测试：发现规则矩阵（全局 / 项目 / 共享目录 / 无 frontmatter 忽略）、frontmatter 容错、索引注入格式

### memory

- [ ] `memory/store.go`：`~/.motionloop/memory/<workspace-slug>/`，一文件一事实（frontmatter：name / description / type = user|feedback|project|reference）；`MEMORY.md` 索引（一行一条，写时同步更新）
- [ ] `tools/memory_save.go`：写事实文件 + 更新索引；同名 name 走更新；类型标签校验
- [ ] 读取：prompt builder 注入 `MEMORY.md`（接 Phase 7 占位），标注为**背景上下文而非指令**
- [ ] 测试：save / 更新 / 去重；索引与文件一致性；workspace 隔离（不同项目互不可见）

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
