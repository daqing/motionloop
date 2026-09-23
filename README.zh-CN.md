# motionloop

用 Go 实现的通用 Agent 框架：自定义系统提示词 + 通用 tool 工具集 + 可插拔的
LLM provider，得到一个适用于任意 agent 场景（而不止编码）的完整
"LLM ⇄ 工具" 循环。

核心 coding agent harness 设计（agent loop、tool 体系、JSONL session、
compaction、skills、memory）参考 [pi](https://github.com/earendil-works/pi)
的实现；motionloop 是**框架优先**——CLI 只是这个库的最大号使用者。运行时代码
零第三方依赖（仅标准库）。

## 安装

要求 Go 1.27+。

```bash
go install github.com/daqing/motionloop/cmd/motionloop@latest
```

或从源码：

```bash
go build ./...
go test ./...
```

## 五分钟上手库

用 provider、model、工具集和系统提示词组装一个 agent：

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	_ "github.com/daqing/motionloop/llm/provider/openaicompat" // 注册 provider
	"github.com/daqing/motionloop/tools"
)

func main() {
	provider, err := llm.Resolve("glm") // 或 "openai"、"deepseek"、"anthropic"
	if err != nil {
		panic(err)
	}
	model := llm.Model{ProviderID: "glm", ModelID: "glm-4.6"}

	ws, _ := tools.NewWorkspace(".")
	a := agent.New(provider, model,
		agent.WithTools(tools.Minimal(ws)...), // bash + read + write
		agent.WithSystemPrompt("You are a helpful assistant."),
		agent.WithAPIKey(os.Getenv("GLM_API_KEY")),
	)

	// 订阅事件流：文本增量、工具执行、用量
	a.Subscribe(func(ev agent.Event) {
		if u, ok := ev.(agent.MessageUpdate); ok {
			if d, ok := u.Delta.(llm.TextDelta); ok {
				fmt.Print(d.Delta)
			}
		}
	})

	if err := a.Prompt(context.Background(), "list the Go files here"); err != nil {
		panic(err)
	}
}
```

一个 `Provider` 接口覆盖所有 OpenAI 兼容端点（OpenAI、GLM、DeepSeek、Kimi、
Ollama、vLLM 等），另有 Anthropic Messages 原生 provider。自定义工具只需实现
一个接口——参数是带 `json`/`jsonschema` tag 的普通 struct，schema 生成与校验
都是现成的。测试可用 `agent.NewFakeProvider` 脚本化响应，完全无网络。

## CLI

```bash
export GLM_API_KEY=...            # 或 OPENAI_API_KEY、ANTHROPIC_API_KEY、MOTIONLOOP_API_KEY

motionloop                         # 交互 REPL：/model、/compact、/fork、/exit
motionloop -p "fix the failing test and run it"
motionloop --provider anthropic --model claude-sonnet-4-5 -p "..."
motionloop --headless -p "..."     # 每行一个 JSON 事件，供程序嵌入

motionloop trust                   # 信任本仓库，允许加载项目级配置
motionloop session ls              # 列出已录制会话
motionloop session show <id>       # entry 树 + 当前链标记
motionloop session fork <id>       # 分叉会话
motionloop session resume <id> -p "continue where we left off"
```

会话以只追加的 JSONL 落在 `~/.motionloop/sessions/`，entry schema 与 pi 兼容
（id/parentId 树）：进程被 kill 后 `resume` 恢复对话；`fork` 无需复制即可分叉。

## 配置

- `~/.motionloop/settings.json` —— 用户级默认：`provider`、`model`、
  `profile`、`compactionThreshold`（估算 token；负值禁用压缩）。受信任项目的
  `.motionloop/settings.json` 再覆盖；`MOTIONLOOP_PROVIDER` /
  `MOTIONLOOP_MODEL` / `MOTIONLOOP_PROFILE` 高于文件；CLI flag 高于一切。
- `~/.motionloop/models.json` —— 不写 Go 代码接入自定义 provider：

  ```json
  {
    "providers": [
      {
        "id": "myllm",
        "baseUrl": "http://localhost:11434/v1",
        "apiKeyEnv": "MYLLM_KEY",
        "type": "openai-compat",
        "models": [{ "id": "llama-x", "contextWindow": 131072, "images": true }]
      }
    ]
  }
  ```

- `~/.motionloop/prompts/coding/<section>.md` —— 覆盖任意系统提示词分区
  （`identity`、`rules` 等）；受信任项目经 `.motionloop/prompts/coding/` 再覆盖。
- Skills：把 [Agent Skills](https://agentskills.io) 包放进 `~/.agents/skills/`
  （跨 harness 共享）或 `~/.motionloop/skills/`；受信任项目可加
  `.agents/skills/`。motionloop 把索引放进系统提示词，模型经 `skills_load`
  工具按需加载全文。
- Memory：长期事实存于 `~/.motionloop/memory/<project>/`（一文件一事实），
  `MEMORY.md` 索引作为背景上下文注入；`memory_save` 工具记录偏好、纠正与
  项目约束。

## 包结构

```
cmd/motionloop    CLI：REPL、单发、headless、session 管理
llm/              消息模型、流事件、provider 注册表
llm/provider/     openaicompat 与 anthropic provider
agent/            循环：工具、事件、hooks、steering、压缩接入
tools/            bash、read、write、edit、grep、glob、ls（+ skills_load、
                  memory_save）、workspace 安全、Coding/Minimal preset
prompt/           命名提示词分区与覆盖链
profile/coding/   内置 coding profile
session/          JSONL 会话（pi entry schema）、fork/branch、compaction
skills/           Agent Skills 发现与索引
memory/           项目级长期记忆
config/           settings 合并、models.json、项目信任
```

依赖方向严格单向：
`cmd → profile → (prompt, tools, skills, memory, session) → agent → llm`。

## 状态

v0.1 —— 规划的 M5 全量功能已完成；架构与决策记录见
[PLAN.md](PLAN.md)，分阶段开发史见 [docs/](docs/)。真实端点 e2e 为可选
（`MOTIONLOOP_E2E=1`）。MIT 协议，见 [LICENSE](LICENSE)。
