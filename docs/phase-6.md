# Phase 6：完整工具集

> 里程碑：M2（第 1/2 步）｜ 前置：Phase 5 ｜ 状态：已完成（2026-09-22）

## 目标

补齐 coding 工具集（write/edit/grep/glob/ls），统一输出约定、截断策略与安全边界（PLAN §5）。

## 任务清单

- [x] `tools/pathutil.go`：`Workspace`（根目录锚定 + **词法**逃逸检测 + 已读文件跟踪 + `Mutate` 串行锁）；read/write/edit/grep/glob/ls 六工具全部经 `resolve` 收口，bash 刻意不锚定
- [x] `tools/write.go`：整文件写入；覆盖已存在文件前要求本会话已读（防盲写）；写入即标记已读（后续 edit 直接可用）；保留既有权限位；自动建父目录
- [x] `tools/edit.go`：精确字符串替换 + 唯一性校验（多匹配报错并列出行号）；成功返回 `@@ path:line @@` 风格的上下文 diff；同样要求已读；`ExecutionMode: Sequential`
- [x] `tools/grep.go`：`pattern` / `glob` / `path`；优先系统 `rg`（exit 1 = 无匹配，rg 故障自动退化），无 rg 时内建遍历（跳隐藏目录/超大文件）；输出 `path:line:text`
- [x] `tools/glob.go`：模式匹配列文件（`**` 跨目录、`*` 限单层，编译为锚定正则），排序，兼负 find 职责，上限 1000
- [x] `tools/ls.go`：目录列表（`d`/`-`/`l name -> target` 标注，隐藏文件默认跳过、`include_hidden` 开启）
- [x] `tools/mutationq.go`：`Workspace.Mutate` 变更串行化（loop 串行批之外的第二道防线，直调 Execute 也安全）
- [x] `tools/truncate.go`：统一输出截断（头 2/3 + 尾 1/3 + 字节数标记）；bash/grep/edit-diff 接入；read 保持行式截断 + `offset` 续读提示
- [x] `tools/registry.go`：`Coding(ws)` 七件套 / `Minimal(ws)` 三件套
- [x] 测试：write 嵌套创建/未读拦截（跨会话与新会话两路径）/目录拒绝；edit 替换+diff/未找到/多匹配行号/未读拒绝；grep 内建+作用域+无匹配+非法 pattern+rg 条件测试；glob `**` vs `*`；ls 标记与隐藏；六工具逃逸拒绝矩阵；Mutate 并发探测=1；Write/Edit 串行模式；preset 装配

## 设计要点

- 输出格式向 pi 对齐（行号前缀、合并输出），模型对这类格式已有训练偏好。
- `Details` 承载 UI 数据（如 edit 的 diff 结构），`Content` 只放模型需要的文本。

## 验收标准

- 全部工具单测绿；`coding` / `minimal` preset 装配正确

## 验证命令

```bash
go test -race ./tools/...
```

## 实施备注（2026-09-22）

- **破坏性变更**：`Read{Root string}` → `Read{WS *Workspace}`（同批迁移 write/edit/grep/glob/ls）；cmd 与既有测试同步更新。库尚无外部使用者，零成本窗口内完成。
- **Workspace 语义**：新建 `NewWorkspace(root)`；逃逸检测是**词法**的（不解析 symlink，文档已注明）；`markRead` 在 read 成功与 write 落盘时触发——write 后 edit 无需再 read。
- **防盲写是会话级的**：已读状态在 Workspace 实例内，不落盘；新会话（新进程/新实例）覆盖旧文件仍需重新 read——保守但正确。
- **grep 的 rg 调用**：`cmd.Dir = base`、路径参数 `.`，保证输出路径相对搜索根，与内建实现显示一致；rg 失败（非 exit 1）自动退化内建，`DisableRipgrep` 字段供测试/极简主机强制内建。
- **glob 语义**：`*` 不跨目录（与 rg `--glob`、gitignore 一致）；`**/*.go` 才递归——测试曾按 `*.go` 期待递归结果，属测试错误。
- **edit 参数名**取 `old_string`/`new_string`（模型训练语料最熟悉的形状）。
