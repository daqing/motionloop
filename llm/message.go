package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Role identifies the participant that produced a message.
type Role string

// Conversation roles understood by providers and persisted in sessions.
const (
	RoleSystem     Role = "system"
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
)

// ContentBlock is one typed block inside a message's content array. The set
// of implementations is closed: TextBlock, ImageBlock, ToolCallBlock, and
// ThinkingBlock.
type ContentBlock interface {
	isContentBlock()
}

var _ []ContentBlock = []ContentBlock{TextBlock{}, ImageBlock{}, ToolCallBlock{}, ThinkingBlock{}}

// TextBlock carries plain text content.
type TextBlock struct {
	Text string `json:"text"`
}

func (TextBlock) isContentBlock() {}

func (b TextBlock) MarshalJSON() ([]byte, error) {
	type alias TextBlock
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{"text", alias(b)})
}

// ImageBlock carries base64-encoded image data.
type ImageBlock struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

func (ImageBlock) isContentBlock() {}

func (b ImageBlock) MarshalJSON() ([]byte, error) {
	type alias ImageBlock
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{"image", alias(b)})
}

// ToolCallBlock is a tool invocation requested by the assistant.
type ToolCallBlock struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (ToolCallBlock) isContentBlock() {}

func (b ToolCallBlock) MarshalJSON() ([]byte, error) {
	type alias ToolCallBlock
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{"toolCall", alias(b)})
}

// ThinkingBlock carries extended-thinking content and its provider signature.
type ThinkingBlock struct {
	Thinking  string `json:"thinking"`
	Signature string `json:"thinkingSignature,omitempty"`
	Redacted  bool   `json:"redacted,omitempty"`
}

func (ThinkingBlock) isContentBlock() {}

func (b ThinkingBlock) MarshalJSON() ([]byte, error) {
	type alias ThinkingBlock
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{"thinking", alias(b)})
}

// ToolDecl declares a callable tool to the model.
type ToolDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Message is one conversational turn. Field names and shapes follow pi's
// session message schema so sessions persist messages verbatim: system
// messages carry prompt sections and tool loadout changes, assistant
// messages carry provider attribution and usage, and tool results reference
// the originating call.
type Message struct {
	Role          Role              `json:"role"`
	Content       []ContentBlock    `json:"content,omitempty"`
	ToolCallID    string            `json:"toolCallId,omitempty"`
	ToolName      string            `json:"toolName,omitempty"`
	IsError       bool              `json:"isError,omitempty"`
	Sections      map[string]string `json:"sections,omitempty"`
	ToolsAdded    []ToolDecl        `json:"toolsAdded,omitempty"`
	ToolsRemoved  []string          `json:"toolsRemoved,omitempty"`
	Provider      string            `json:"provider,omitempty"`
	Model         string            `json:"model,omitempty"`
	ResponseModel string            `json:"responseModel,omitempty"`
	ThinkingLevel string            `json:"thinkingLevel,omitempty"`
	StopReason    StopReason        `json:"stopReason,omitempty"`
	Usage         *Usage            `json:"usage,omitempty"`
	ErrorMessage  string            `json:"errorMessage,omitempty"`
	Timestamp     int64             `json:"timestamp"`
}

// UnmarshalJSON decodes a message, accepting content as either an array of
// typed blocks or a bare string (normalized to a single TextBlock).
func (m *Message) UnmarshalJSON(data []byte) error {
	type alias Message
	var raw struct {
		alias
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = Message(raw.alias)
	blocks, err := unmarshalBlocks(raw.Content)
	if err != nil {
		return err
	}
	m.Content = blocks
	return nil
}

func unmarshalBlocks(data []byte) ([]ContentBlock, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		return []ContentBlock{TextBlock{Text: s}}, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(trimmed, &raws); err != nil {
		return nil, fmt.Errorf("llm: decode content blocks: %w", err)
	}
	blocks := make([]ContentBlock, 0, len(raws))
	for _, r := range raws {
		b, err := unmarshalBlock(r)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
}

func unmarshalBlock(data []byte) (ContentBlock, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case "text":
		var b TextBlock
		return b, json.Unmarshal(data, &b)
	case "image":
		var b ImageBlock
		return b, json.Unmarshal(data, &b)
	case "toolCall":
		var b ToolCallBlock
		return b, json.Unmarshal(data, &b)
	case "thinking":
		var b ThinkingBlock
		return b, json.Unmarshal(data, &b)
	default:
		return nil, fmt.Errorf("llm: unknown content block type %q", head.Type)
	}
}
