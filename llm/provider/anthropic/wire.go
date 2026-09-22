package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/daqing/motionloop/llm"
)

type antRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int64        `json:"max_tokens"`
	System      []antBlock   `json:"system,omitempty"`
	Messages    []antMessage `json:"messages"`
	Tools       []antTool    `json:"tools,omitempty"`
	Stream      bool         `json:"stream"`
	Temperature *float64     `json:"temperature,omitempty"`
	Thinking    *antThinking `json:"thinking,omitempty"`
}

type antThinking struct {
	Type         string `json:"type"`
	BudgetTokens int64  `json:"budget_tokens"`
}

type antMessage struct {
	Role    string     `json:"role"`
	Content []antBlock `json:"content"`
}

type antBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// image
	Source *antSource `json:"source,omitempty"`
	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// prompt caching
	CacheControl *antCacheControl `json:"cache_control,omitempty"`
}

type antSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type antCacheControl struct {
	Type string `json:"type"`
}

type antTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type antUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

func (u *antUsage) apply(dst *llm.Usage) {
	dst.Input += u.InputTokens
	dst.CacheWrite += u.CacheCreationInputTokens
	dst.CacheRead += u.CacheReadInputTokens
	dst.Output += u.OutputTokens
	dst.TotalTokens = dst.Input + dst.CacheWrite + dst.CacheRead + dst.Output
}

// convertMessages maps the motionloop transcript to Messages API shapes:
// system messages merge into the top-level system field (empty-content
// patch messages contribute nothing), tool results become user messages,
// and consecutive same-role messages merge their content arrays.
func convertMessages(messages []llm.Message) (system []antBlock, out []antMessage, err error) {
	appendMerged := func(role string, blocks []antBlock) {
		if len(out) > 0 && out[len(out)-1].Role == role {
			out[len(out)-1].Content = append(out[len(out)-1].Content, blocks...)
			return
		}
		out = append(out, antMessage{Role: role, Content: blocks})
	}
	for _, m := range messages {
		switch m.Role {
		case llm.RoleSystem:
			if s := textContent(m.Content); strings.TrimSpace(s) != "" {
				system = append(system, antBlock{Type: "text", Text: s})
			}
		case llm.RoleUser:
			var blocks []antBlock
			for _, b := range m.Content {
				switch blk := b.(type) {
				case llm.TextBlock:
					blocks = append(blocks, antBlock{Type: "text", Text: blk.Text})
				case llm.ImageBlock:
					blocks = append(blocks, antBlock{Type: "image", Source: &antSource{
						Type:      "base64",
						MediaType: blk.MimeType,
						Data:      blk.Data,
					}})
				default:
					return nil, nil, fmt.Errorf("anthropic: user message cannot carry %T", b)
				}
			}
			appendMerged("user", blocks)
		case llm.RoleAssistant:
			var blocks []antBlock
			for _, b := range m.Content {
				switch blk := b.(type) {
				case llm.TextBlock:
					if blk.Text != "" {
						blocks = append(blocks, antBlock{Type: "text", Text: blk.Text})
					}
				case llm.ThinkingBlock:
					blocks = append(blocks, antBlock{Type: "thinking", Thinking: blk.Thinking, Signature: blk.Signature})
				case llm.ToolCallBlock:
					input := blk.Arguments
					if len(input) == 0 {
						input = json.RawMessage("{}")
					}
					blocks = append(blocks, antBlock{Type: "tool_use", ID: blk.ID, Name: blk.Name, Input: input})
				}
			}
			appendMerged("assistant", blocks)
		case llm.RoleToolResult:
			block := antBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				IsError:   m.IsError,
			}
			if s := textContent(m.Content); s != "" {
				block.Text = s
			}
			appendMerged("user", []antBlock{block})
		default:
			return nil, nil, fmt.Errorf("anthropic: unsupported role %q", string(m.Role))
		}
	}
	if len(system) == 0 {
		system = nil
	}
	return system, out, nil
}

func textContent(blocks []llm.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if tb, ok := b.(llm.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}
