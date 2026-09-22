package openaicompat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/daqing/motionloop/llm"
)

type chatRequest struct {
	Model           string             `json:"model"`
	Messages        []chatMessage      `json:"messages"`
	Tools           []chatTool         `json:"tools,omitempty"`
	MaxTokens       int64              `json:"max_tokens,omitempty"`
	Temperature     float64            `json:"temperature,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	Stream          bool               `json:"stream"`
	StreamOptions   *chatStreamOptions `json:"stream_options,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function chatToolCallFnSpec `json:"function"`
}

type chatToolCallFnSpec struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string         `json:"type"`
	Function chatToolFnSpec `json:"function"`
}

type chatToolFnSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *chatImageURL `json:"image_url,omitempty"`
}

type chatImageURL struct {
	URL string `json:"url"`
}

type chatChunk struct {
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage"`
}

type chatChoice struct {
	Delta        chatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type chatDelta struct {
	Content          string              `json:"content"`
	ReasoningContent string              `json:"reasoning_content"`
	Reasoning        string              `json:"reasoning"`
	ToolCalls        []chatToolCallDelta `json:"tool_calls"`
}

type chatToolCallDelta struct {
	Index    int                `json:"index"`
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function chatToolCallFnSpec `json:"function"`
}

type chatUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func chatMessages(messages []llm.Message) ([]chatMessage, error) {
	out := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		cm, err := chatMessageFrom(m)
		if err != nil {
			return nil, err
		}
		out = append(out, cm)
	}
	return out, nil
}

func chatMessageFrom(m llm.Message) (chatMessage, error) {
	switch m.Role {
	case llm.RoleSystem:
		return chatMessage{Role: "system", Content: textContent(m.Content)}, nil
	case llm.RoleUser:
		if onlyText(m.Content) {
			return chatMessage{Role: "user", Content: textContent(m.Content)}, nil
		}
		parts := make([]chatPart, 0, len(m.Content))
		for _, b := range m.Content {
			switch blk := b.(type) {
			case llm.TextBlock:
				parts = append(parts, chatPart{Type: "text", Text: blk.Text})
			case llm.ImageBlock:
				parts = append(parts, chatPart{
					Type:     "image_url",
					ImageURL: &chatImageURL{URL: "data:" + blk.MimeType + ";base64," + blk.Data},
				})
			default:
				return chatMessage{}, fmt.Errorf("openaicompat: user message cannot carry %T", b)
			}
		}
		return chatMessage{Role: "user", Content: parts}, nil
	case llm.RoleAssistant:
		cm := chatMessage{Role: "assistant"}
		if s := textContent(m.Content); s != "" {
			cm.Content = s
		}
		for _, b := range m.Content {
			if tc, ok := b.(llm.ToolCallBlock); ok {
				cm.ToolCalls = append(cm.ToolCalls, chatToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: chatToolCallFnSpec{Name: tc.Name, Arguments: string(tc.Arguments)},
				})
			}
		}
		return cm, nil
	case llm.RoleToolResult:
		if m.ToolCallID == "" {
			return chatMessage{}, fmt.Errorf("openaicompat: tool result for %q is missing toolCallId", m.ToolName)
		}
		return chatMessage{Role: "tool", Content: textContent(m.Content), ToolCallID: m.ToolCallID}, nil
	default:
		return chatMessage{}, fmt.Errorf("openaicompat: unsupported role %q", string(m.Role))
	}
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

func onlyText(blocks []llm.ContentBlock) bool {
	for _, b := range blocks {
		if _, ok := b.(llm.TextBlock); !ok {
			return false
		}
	}
	return true
}

func chatTools(tools []llm.ToolDecl) []chatTool {
	out := make([]chatTool, 0, len(tools))
	for _, td := range tools {
		out = append(out, chatTool{
			Type:     "function",
			Function: chatToolFnSpec{Name: td.Name, Description: td.Description, Parameters: td.Parameters},
		})
	}
	return out
}

func (c *chatChunk) events() []llm.StreamEvent {
	var evs []llm.StreamEvent
	for _, choice := range c.Choices {
		if choice.Delta.Content != "" {
			evs = append(evs, llm.TextDelta{Delta: choice.Delta.Content})
		}
		if d := choice.Delta.ReasoningContent; d != "" {
			evs = append(evs, llm.ThinkingDelta{Delta: d})
		}
		if d := choice.Delta.Reasoning; d != "" {
			evs = append(evs, llm.ThinkingDelta{Delta: d})
		}
		for _, tc := range choice.Delta.ToolCalls {
			evs = append(evs, llm.ToolCallDelta{
				Index:          tc.Index,
				ID:             tc.ID,
				Name:           tc.Function.Name,
				ArgumentsDelta: tc.Function.Arguments,
			})
		}
	}
	if c.Usage != nil {
		evs = append(evs, llm.UsageUpdate{Usage: usageFromChat(c.Usage)})
	}
	return evs
}

func (c *chatChunk) finishReason() string {
	for _, choice := range c.Choices {
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			return *choice.FinishReason
		}
	}
	return ""
}

func usageFromChat(u *chatUsage) llm.Usage {
	out := llm.Usage{
		Input:       u.PromptTokens,
		Output:      u.CompletionTokens,
		TotalTokens: u.TotalTokens,
	}
	if u.PromptTokensDetails != nil {
		out.CacheRead = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil {
		out.Reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	return out
}

func reasoningEffort(level llm.ThinkingLevel) string {
	switch level {
	case llm.ThinkingMinimal, llm.ThinkingLow:
		return "low"
	case llm.ThinkingMedium:
		return "medium"
	case llm.ThinkingHigh:
		return "high"
	default:
		return ""
	}
}

func stopReasonOf(finish string) llm.StopReason {
	switch finish {
	case "length":
		return llm.StopReasonLength
	case "tool_calls", "tool_use", "function_call":
		return llm.StopReasonToolUse
	default:
		return llm.StopReasonStop
	}
}
