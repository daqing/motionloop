# Motionloop 项目规划

> 状态：v1.1，review 完成——全部开放问题已决策（2026-09-22），可按 M0 开工。
> 本文档基于对 [earendil-works/pi](https://github.com/earendil-works/pi)（pi.dev，原 badlogic/pi-mono）源码的调研，梳理其核心 harness 设计后形成。

## 1. 项目定位

**Motionloop 是一个用 Go 实现的通用 Agent 框架**：提供可自定义系统提示词、通用 tool 工具集、可扩展 LLM provider 插件三大能力，任何"LLM + 工具循环"的场景都能基于它构建。

核心 coding agent 的设计完全参考 Pi.dev 的实现，但仅取其**核心 harness 部分**（agent loop、tool 体系、session、compaction、skills、memory 等），不参考其 TUI、telemetry、durable runtime 等外围设施。

与 pi 的关键定位差异：

| | pi | motionloop |
|---|---|---|
| 形态 | 一个 coding agent CLI，SDK 是顺带产物 | **框架优先**（Go library），CLI 是参考实现 |
| 默认场景 | coding agent 一种 | 任意 agent 场景，coding agent 是内置的一个 profile |
| 语言生态 | TypeScript | Go（单二进制、易嵌入、交叉编译） |

**Non-goals（v0.x 明确不做）：**

- 不做 TUI 框架（交互模式用行式输出 + readline 级别即可；TUI 整体留待以后、暂不规划——Q3 已决策）
- 不做内置权限/沙箱系统（沿用 pi 的模型：以当前用户权限运行，沙箱交给外部容器）
- 不做多 agent 编排/子代理调度（后续可作为框架之上的库实现）
- 不做 provider 侧的训练/微调设施

## 2. 参考：pi 的核心架构（我们借鉴什么）

pi monorepo 分三层，motionloop 一一对应：

| pi 包 | 职责 | motionloop 对应 |
|---|---|---|
| `pi-ai` | 统一多 provider LLM API：消息模型（system/user/assistant/toolResult + text/image/toolCall 内容块）、流式事件流、usage/缓存统计、model catalog、OAuth/API key | `llm` 包 + `llm/provider/*` |
| `pi-agent-core` | Agent 运行时：agent loop（LLM 调用 ⇄ 工具执行）、事件流（agent/turn/message/tool_execution 三级生命周期）、hooks（beforeToolCall/afterToolCall/prepareRequest/finishTurn）、steering/followUp 队列、并行/串行工具执行、transformContext/convertToLlm 上下文管道 | `agent` 包 |
| `pi-coding-agent` | 产品层：bash/read/edit/write/grep/find/ls 工具、系统提示词组装、skills（Agent Skills 标准）、JSONL session、compaction、extensions、settings | `tools` + `prompt` + `skills` + `session` + `config` 包 + coding profile |

**明确借鉴的设计**（来自源码调研）：

- `AgentTool` 接口：schema + `execute(toolCallId, args, signal, onUpdate)`，结果分 `content`（回传模型）与 `details`（给 UI/日志的结构化数据），`terminate` 提示提前结束循环。
- 三级事件生命周期：`agent_start/end` → `turn_start/end` → `message_start/update/end` + `tool_execution_start/update/end`，这是 CLI、headless、测试共用的观测面。
- 工具执行模式：parallel（默认，按完成序发事件、按源序持久化）与 sequential（per-tool 覆盖，如写文件类工具）。
- 消息管道：`AgentMessage[] → transformContext() → convertToLlm() → Message[]`，把"应用自有消息类型"与"LLM 可见消息"解耦，compaction/裁剪发生在 transform 层。
- 系统提示词即 transcript：system prompt 与工具声明放在会话头部 system message 中，后续 system message 可以增量 patch，会话重放即状态重建。
- Skills：实现 [agentskills.io](https://agentskills.io) 标准（SKILL.md + frontmatter，全局/项目多级目录发现，按需注入索引）。

## 3. 总体架构

### 3.1 包结构与依赖方向

```
motionloop/
├── cmd/motionloop/          # CLI 入口（薄壳，只做装配）
├── llm/                     # LLM 统一抽象（对标 pi-ai）
│   ├── message.go           #   Message / ContentBlock / ToolCall / Usage
│   ├── model.go             #   Model 元数据与 catalog
│   ├── stream.go            #   StreamEvent / StreamFn 约定
│   ├── provider.go          #   Provider 接口 + registry
│   └── provider/
│       ├── openaicompat/    #   OpenAI-compatible 通用 provider（覆盖面最大）
│       ├── anthropic/       #   Anthropic Messages API 原生 provider
│       └── registry.go      #   编译期注册（blank import）
├── agent/                   # Agent 运行时（对标 pi-agent-core）
│   ├── agent.go             #   Agent 结构与状态
│   ├── loop.go              #   核心循环
│   ├── tool.go              #   Tool 接口、结果类型、执行调度（并行/串行）
│   ├── event.go             #   事件定义与订阅
│   └── hook.go              #   beforeToolCall/afterToolCall/finishTurn 等
├── tools/                   # 内置工具集（对标 pi-coding-agent/src/core/tools）
│   ├── bash.go / read.go / write.go / edit.go / grep.go / glob.go / ls.go
│   └── registry.go          #   工具分组：coding preset / minimal preset
├── prompt/                  # 系统提示词体系
│   ├── template.go          #   模板 + 分区（sections）
│   └── builder.go           #   动态注入（cwd、git 状态、skills 索引、日期）
├── profile/                 # 场景 profile：把 prompt + tools + 默认参数打包
│   └── coding/coding.go     #   内置 coding agent profile（参考 pi 的完整提示词工程）
├── session/                 # 会话持久化
│   ├── format.go            #   JSONL 格式与读写
│   ├── manager.go           #   列出/resume/branch
│   └── compact.go           #   compaction（摘要压缩）
├── skills/                  # Agent Skills 标准实现
├── memory/                  # 长期记忆子系统（见 §9）
├── config/                  # settings / models 配置、多级目录合并
└── main.go                  # 删除，逻辑移入 cmd/motionloop
```

依赖方向自上而下单向：`cmd → profile → (prompt, tools, skills, memory, session) → agent → llm`。
`agent` 与 `llm` 不依赖任何上层；`tools` 只依赖 `agent` 的 Tool 接口。

### 3.2 关键 Go 化设计决策

| pi (TypeScript) | motionloop (Go) | 说明 |
|---|---|---|
| `AbortSignal` | `context.Context` | 贯穿 Provider.Stream、Tool.Execute、Agent.Prompt |
| `subscribe(listener)` 事件回调 | 订阅接口 `Subscribe(fn Event)` + 可选 channel | 回调为主，channel 适配 headless 场景 |
| typebox schema | Go struct tag 声明 + 反射生成 JSON Schema | 工具参数用结构体定义，一次声明同时得到校验和 schema |
| async iterator 事件流 | `<-chan StreamEvent` 或回调 | Provider 流式接口两种都提供，loop 内部用 channel |
| promise 并发 | `errgroup` + `context` | 并行工具执行、超时取消 |
| declaration merging 扩展消息类型 | `AgentMessage` 用 interface + 类型断言 | 自定义消息类型经 `ConvertToLLM` 过滤/转换 |
| TS extension 模块热加载 | 编译期注册（blank import） | 见 §4.3；不做进程外插件（Q1 已决策） |

## 4. 核心抽象

### 4.1 `llm` 包：消息模型与 Provider 接口

```go
// Message 是 LLM 可见的消息。system prompt 与工具声明由头部 system message 携带。
type Message struct {
    Role       Role          // system / user / assistant / toolResult
    Content    []ContentBlock // text / image / toolCall
    ToolCallID string         // toolResult 专用
    // ...
}

type Provider interface {
    // Stream 发起一次对话补全。失败不得返回 error（除非 ctx 取消），
    // 协议错误通过事件流中的 error 事件 + 最终 stopReason 表达（对齐 pi 的 StreamFn 契约）。
    Stream(ctx context.Context, model Model, messages []Message, opts StreamOptions) (<-chan StreamEvent, error)
    // Capabilities 声明支持的能力：thinking、image、cache、并行 tool call 等。
    Capabilities(model Model) Capabilities
}

// 全局注册表：provider 包 init() 中调用 Register，二进制通过 blank import 决定装了哪些。
func Register(p Provider)
func Resolve(providerID string) (Provider, error)
```

模型元数据（上下文窗口、价格、能力）来自内置 catalog + 用户 `models.json` 覆盖（对齐 pi 的 custom provider 机制）。

**Provider 实现矩阵**：v0.1 只做两个——`openaicompat`（一个实现覆盖 OpenAI/DeepSeek/GLM/Kimi/Ollama/vLLM 等一切兼容端点，靠配置 baseURL + 模型列表）和 `anthropic`（原生 Messages API，覆盖 thinking/cache/tool-use 细节）。其余按需加。

### 4.2 `agent` 包：Tool 接口与循环

```go
// Tool 是 motionloop 的扩展原子。参数用 struct tag 声明 schema。
type Tool interface {
    Name() string
    Description() string
    Parameters() jsonschema.Schema // 由 struct tag 反射生成，可缓存
    // Execute 失败时返回 error 由 loop 统一转为错误结果；不应 panic。
    Execute(ctx context.Context, call ToolCall, emit func(Update)) (Result, error)
}

// Result 区分回传模型的内容与给 UI 的结构化细节（对齐 AgentToolResult）。
type Result struct {
    Content   []llm.ContentBlock // 回传给模型
    Details   any                // UI/日志渲染用，不进上下文
    Terminate bool               // 提示本批工具结果后提前结束循环
}

type Agent struct { /* state: model, tools, messages, thinking level */ }

// Prompt 驱动一次完整 run：user 消息 → LLM 流式响应 → 并行/串行执行工具
// → tool results → 下一轮，直到无工具调用、Terminate、错误或 ctx 取消。
func (a *Agent) Prompt(ctx context.Context, input string) error

// Hooks（对齐 pi）：BeforeToolCall（可拦截）、AfterToolCall（可改写结果）、
// TransformContext（compaction/裁剪）、ConvertToLLM（自定义消息类型过滤）、
// PrepareRequest（每次请求前重建上下文）、FinishTurn（end/continue 决策）。
```

循环内职责边界（v1 实现范围）：

- 工具执行调度：默认并行（errgroup），per-tool `executionMode` 可强制串行（文件写入类）。
- steering：run 进行中排队的新 user 消息，在轮次边界注入（pi 的 one-at-a-time 模式）。
- 错误策略：LLM 错误按 stopReason=error 结束 run 并保留事件序列完整；工具错误转为 isError 的 toolResult 继续循环。

### 4.3 扩展机制（框架的"插件" story）

分两层，**不做子进程插件协议（MCP 之类），保持简单**（已决策，见 §15 Q1）：

1. **Go 库使用者**（主要形态）：实现 `Tool`/`Provider` 接口、注册 registry、组装 Agent。框架本身就是插件体系，CLI 只是最大号的使用者。
2. **CLI 使用者**（不开 Go 代码）：通过配置达成——自定义系统提示词（`--system-prompt` / 项目配置）、`models.json` 加自定义 provider 端点、skills 目录加能力；脚本类外部能力由 `bash` 工具天然覆盖。

## 5. 内置工具集

对标 `pi-coding-agent` 的 7 个核心工具，输出约定尽量对齐（模型已被这类格式训练过）：

| 工具 | 要点 |
|---|---|
| `read` | 行号前缀格式、头部截断（默认 2000 行）、支持图片（base64）、读取范围参数 |
| `bash` | 超时与后台运行、输出合并 stdout/stderr、工作目录 |
| `write` | 整文件写入，前置已读检测（防止覆盖未读文件） |
| `edit` | 精确字符串替换 + 唯一性校验（pi 的 edit 语义），带 diff 输出 |
| `grep` | ripgrep 兼容参数子集；系统无 rg 时退化到内建实现 |
| `glob` / `find` | 模式匹配列文件 |
| `ls` | 目录列表 |

工具集按 preset 分组：`coding`（上述全部）、`minimal`（bash/read/write，用于通用 agent 场景）、空集（纯对话）。preset 是 profile 的组成部分，库使用者也可以任意挑单个工具注册。

工具实现的输出截断、路径安全（限制在 workspace 内）、并发文件写串行化，都参考 pi 的对应实现细节（`truncate.ts`、`file-mutation-queue.ts`、`path-utils.ts`）。

## 6. 系统提示词体系

框架级能力（这是"可自定义系统提示词"的落地）：

- **模板 + 分区**：系统提示词由命名 sections 组成（如 `identity` / `environment` / `skills` / `rules`），支持增量替换，对齐 pi 的 `sections` patch 机制。
- **动态注入**：builder 在会话启动时注入 cwd、平台、日期、git 状态、skills 索引、memory 摘要等环境事实。
- **三级覆盖**：内置默认 → `~/.motionloop/prompts/` 全局 → `.motionloop/prompts/` 项目级；CLI `--system-prompt` 直接整体替换。
- coding profile 内置一套完整的 coding agent 系统提示词（行为准则、工具使用规范、git 约定），此部分设计参考 pi 的 `system-prompt.ts`。

## 7. Session 与上下文管理

- **格式（已定）**：JSONL，每行一个 entry，只追加。崩溃撕裂的最后一行直接丢弃；写入原子性靠 temp+rename 的 compaction 间隙处理。
- **entry schema（已定，采用 pi 的 session-format）**：
  - 首行 `SessionHeader`：`{"type":"session","version":N,"id","cwd",...}`，文件级元数据，不属于消息树；fork 出的会话带 `parentSession` 指回源文件。
  - 信封字段：除 header 外每行都带 `type` / `id` / `parentId` / `timestamp`，线性日志由此构成**树**——分支（fork、回退后重试）原地挂新链，不开新文件。
  - entry 类型：`message`（主体）+ `model_change`、`thinking_level_change`、`usage` 等事件类；motionloop 特有事件（memory 写入、profile 切换等）以新增 `type` 扩展，不改核心结构。
  - `message` 内部：role（system/user/assistant/toolResult/custom…）+ content blocks（text/image/thinking/toolCall）；system message 携带 prompt 分区（`sections`）与工具增删（`toolsAdded`/`toolsRemoved`），**重放全文件即重建当前提示词与工具状态**，无独立状态文件。
  - `version` 字段 + 加载时自动迁移（对齐 pi v1→v3 的演进方式）。
  - 定位是**结构性采用**，不承诺字节级兼容 pi 的会话文件。
- **能力**：resume（重放重建状态）、fork/branch（树内原地分支）、导出。
- **compaction**：上下文接近窗口上限时，把旧消息摘要成一条 summary system message（对齐 pi 的 compaction 思路），触发阈值与摘要模型可配。
- v0.1 只做文件 backend；接口留出 SQLite 等实现的余地（pi 把 session backend 拆成独立包的做法值得抄）。

## 8. Skills（Agent Skills 标准）

按 [agentskills.io](https://agentskills.io) 规范实现，兼容 Claude Code / pi / ZCode 等生态的现有 skills 目录：

- 发现路径：`~/.motionloop/skills/`、`~/.agents/skills/`（跨 harness 共享）、项目 `.motionloop/skills/`（需项目信任确认）。
- `SKILL.md` frontmatter（name/description）解析与校验，轻量容错（对齐 pi 的 lenient 策略）。
- 注入方式：只把 name+description 索引进系统提示词，模型按需要求加载全文（渐进式披露）。
- 提供框架级 API：`skills.Load(dirs...)`、`skills.Index()` 供 prompt builder 消费。

## 9. Memory（长期记忆）

pi 本体没有内置持久 memory（其上下文复用靠 sessions + skills），motionloop 按业界已验证的 harness memory 模式自建，作为框架一等能力：

- **分层**：session 短期记忆（自动，就是会话本身）＋ 项目级长期记忆（`memory/` 目录，一个事实一个文件，带 frontmatter 与类型标签：user/feedback/project/reference）＋ `MEMORY.md` 索引（每次会话注入）。
- **写入路径**：内置 `memory_save` 工具（写文件 + 更新索引），由系统提示词约定何时使用；去重/更新/失效由工具实现负责。
- **读取路径**：prompt builder 自动注入 `MEMORY.md`；recalled 记忆作为背景上下文标注，不作为指令。
- **作用域**：按 workspace（项目路径 hash）隔离，`~/.motionloop/memory/<project>/`。

## 10. 配置体系

- 全局：`~/.motionloop/`（`settings.json`、`models.json`、`prompts/`、`skills/`、`memory/`、`sessions/`）
- 项目：`.motionloop/`（同名子集，仅信任后生效——对齐 pi 的 project trust 机制）
- 环境变量：`MOTIONLOOP_*`（provider API key 走各厂商惯例变量：`ANTHROPIC_API_KEY`、`OPENAI_API_KEY` 等）
- `models.json`：自定义 provider/model 条目（baseURL、模型列表、能力声明），是"provider 插件"在不开 Go 代码时的配置面。
- 配置文件格式**暂定 JSON**：与 session 容器统一、零依赖；若后续确有手写大段配置的痛点，再评估引入 TOML。

## 11. CLI 与交付形态

```
motionloop                      # 交互式 REPL（行式输出 + 工具执行实时展示）
motionloop -p "修复这个测试"     # 单发非交互，stdout 打印结果
motionloop --headless           # JSON 事件流输出（供程序包装，对齐 pi 的 rpc/json 模式）
motionloop session ls/resume/fork
motionloop --profile coding --system-prompt ./my-prompt.md
```

库形态（框架本体）：`import "github.com/daqing/motionloop/agent"` 组装自己的 agent，是 README 的第一个示例。

## 12. 里程碑

每个里程碑结束时仓库可构建、测试绿、能演示。

| 里程碑 | 内容 | 验收标准 |
|---|---|---|
| **M0 骨架 + 最小回路** | `llm` 消息模型与 StreamEvent；`openaicompat` provider；`agent` 最小 loop（串行工具）；`tools` 先只有 bash/read | `go run ./cmd/motionloop -p "..."` 在测试端点上完成一次"读文件→回答"；fake provider 单测覆盖 loop 状态机 |
| **M1 agent core 完整** | 事件系统、hooks（before/afterToolCall、transformContext、finishTurn）、并行工具执行、steering；session JSONL：pi 式 entry schema（header/树/重放），写/重放/resume/fork | 事件序列单测对齐 pi 的事件顺序；kill 进程后 resume 恢复对话；fork 分支后两条支线独立演进、各自重放的提示词与工具状态正确 |
| **M2 工具集 + 提示词体系** | 7 工具齐 + 输出约定；prompt 模板/分区/动态注入；`profile/coding` | 用 coding profile 端到端完成一次真实小任务（读代码→改→跑测试）；工具单测含截断/路径边界 |
| **M3 provider 矩阵 + 配置** | `anthropic` provider；models.json 自定义 provider；settings 三级合并；project trust | 双 provider 各跑通 e2e；仅靠 models.json 接入一个第三方 OpenAI 兼容端点 |
| **M4 skills + memory + compaction** | skills 发现/索引/加载；memory 子系统与工具；上下文 compaction | 加载一组现有 `~/.agents/skills`；长会话触发 compaction 后可继续 |
| **M5 打磨 + v0.1** | REPL 交互细节、headless JSON 流、README（英/中）、CI | 发布 v0.1 tag |

## 13. 测试策略

- **loop 状态机**：fake provider（脚本化返回序列）驱动，断言完整事件序列与 session 内容，不依赖网络。
- **provider 兼容性**：`httptest` 假服务器回放各厂商响应报文（SSE），golden 断言解析结果。
- **工具**：真实临时目录操作，覆盖截断、权限、路径逃逸、并发写。
- **session 格式**：golden 文件，格式变更显式可见。
- **e2e 冒烟**：M2 起保留一个可选的真实模型脚本（读 env key 才跑），CI 默认跳过。

## 14. 依赖与工程实践

依赖策略：运行时零第三方依赖（标准库 `net/http`、`encoding/json` 足够）；JSON Schema 生成自研轻量反射实现（工具参数场景够用，不引大库）。测试工具链按需引入（如 `testify` 可接受）。CLI readline 交互优先用纯 Go 轻量实现，不引 bubbletea（TUI 暂不规划）。

Go 工程实践（整体跟进主流最佳实践，CI 强制）：

- 布局：根级公开包（无 `pkg/` 前缀）；确属内部的装配代码放 `internal/`。
- 格式与静态检查：`gofmt` / `goimports` 必过；`go vet` + `staticcheck`（经 `golangci-lint`）进 CI。
- 错误处理：`fmt.Errorf("...: %w")` 包装保留错误链，`errors.Is/As` 判定；业务错误不走 panic。
- 并发：`context.Context` 一律为首参并贯穿取消传播；goroutine 生命周期归属明确（`errgroup` 管理）。
- 测试：table-driven 风格、可并行处 `t.Parallel()`、golden 文件锁定 wire 格式。
- API 纪律：v0 阶段允许迭代，但导出面保持小；破坏性变更在 release note 标注。
- 文档：公开包维护 `doc.go` 与示例（`example_test.go`），`go doc` 可读。

## 15. 决策记录（原开放问题，review 已全部决策）

1. **子进程插件机制（Q1）——已决策（2026-09-22）**：不做 MCP 之类的子进程插件协议，保持简单。扩展只保留两条路径：Go 接口注册（库使用者）+ 配置面（提示词 / `models.json` / skills）；脚本类外部能力由 `bash` 工具覆盖。若未来确有强需求再评估，且届时优先实现 MCP client 而非自造协议（当前无此计划）。
2. **Entry schema（Q2）——已决策（2026-09-22）**：采用 pi 的 entry schema，结构性采用、不做字节级兼容承诺。容器 JSONL + pi 的信封（type/id/parentId/timestamp 树结构）+ 首行 SessionHeader + system message 承载 prompt 分区与工具增删，细节见 §7。
3. **交互 UI 范围（Q3）——已决策（2026-09-22）**：TUI 留待以后，暂不规划（不占任何里程碑）。v0.x 交互模式保持行式输出 + readline，不引入 bubbletea 等 TUI 依赖。
4. **Go 布局（Q4）——已决策（2026-09-22）**：根级包（无 `pkg/` 前缀），并整体跟进主流 Go 工程实践（详见 §14）。
5. **Memory 设计（Q5）——已决策（2026-09-22）**：按 §9 方案执行——一个事实一个文件 + `MEMORY.md` 索引 + `memory_save` 工具，按 workspace 隔离。调研结论备忘：pi 本体没有产品化的持久 memory 能力（仓库里的 "memory" 是会话存储的内存 backend 实现与 harness 实验中的 durable storage 原型；其上下文复用实际靠 sessions + skills），因此不存在"对齐 pi memory"的选项，按业界已验证的 harness memory 模式自建。
