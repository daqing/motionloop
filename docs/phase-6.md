# Phase 6：完整工具集

> 里程碑：M2（第 1/2 步）｜ 前置：Phase 5 ｜ 状态：未开始

## 目标

补齐 coding 工具集（write/edit/grep/glob/ls），统一输出约定、截断策略与安全边界（PLAN §5）。

## 任务清单

- [ ] `tools/write.go`：整文件写入；覆盖已存在文件前要求该文件在会话中被 read 过（防盲写）
- [ ] `tools/edit.go`：精确字符串替换 + 唯一性校验（多处匹配时报错并列出位置）；成功后返回 diff
- [ ] `tools/grep.go`：`pattern` / `glob` / `path` 参数；优先调系统 `rg`，缺失则退化到内建遍历 + regexp
- [ ] `tools/glob.go`：模式匹配列文件（支持 `**`），兼负 find 职责；结果排序
- [ ] `tools/ls.go`：目录列表（文件/目录/链接标注，隐藏文件参数）
- [ ] `tools/pathutil.go`：workspace 逃逸检测——read/write/edit/glob/grep 限制在 workspace 内，bash 明确不限制
- [ ] `tools/mutationq.go`：文件变更串行化队列；write/edit 标记 `ExecutionMode: Sequential`
- [ ] `tools/truncate.go`：统一输出截断（字节上限 + 截断标记，对齐 pi 的 truncate 策略）
- [ ] `tools/registry.go`：preset 分组——`coding`（全量七件）/ `minimal`（bash+read+write）/ 空
- [ ] 测试：每工具 happy path + 边界——截断、路径逃逸拒绝、edit 多匹配、grep 无 rg 退化、并发写串行化、write 未读拦截

## 设计要点

- 输出格式向 pi 对齐（行号前缀、合并输出），模型对这类格式已有训练偏好。
- `Details` 承载 UI 数据（如 edit 的 diff 结构），`Content` 只放模型需要的文本。

## 验收标准

- 全部工具单测绿；`coding` / `minimal` preset 装配正确

## 验证命令

```bash
go test -race ./tools/...
```
