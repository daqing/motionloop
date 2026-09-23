package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// renderHeadlessEvent prints one JSON object per line — the embeddable CLI
// contract. REPL and headless consume the same event stream; this is just
// another renderer.
func renderHeadlessEvent(ev agent.Event) {
	out := func(obj map[string]any) {
		line, err := json.Marshal(obj)
		if err != nil {
			return
		}
		fmt.Println(string(line))
	}
	switch e := ev.(type) {
	case agent.AgentStart:
		out(map[string]any{"type": "agent_start"})
	case agent.AgentEnd:
		out(map[string]any{"type": "agent_end", "messageCount": len(e.Messages)})
	case agent.TurnStart:
		out(map[string]any{"type": "turn_start"})
	case agent.TurnEnd:
		out(map[string]any{"type": "turn_end", "toolResultCount": len(e.ToolResults)})
	case agent.MessageStart:
		out(map[string]any{"type": "message_start", "role": string(e.Message.Role)})
	case agent.MessageUpdate:
		obj := map[string]any{"type": "message_update", "role": string(e.Message.Role)}
		if d, ok := e.Delta.(llm.TextDelta); ok {
			obj["delta"] = d.Delta
		}
		out(obj)
	case agent.MessageEnd:
		obj := map[string]any{"type": "message_end", "role": string(e.Message.Role)}
		if s := messageText(e.Message); s != "" {
			obj["text"] = s
		}
		if e.Message.StopReason != "" {
			obj["stopReason"] = string(e.Message.StopReason)
		}
		if e.Message.IsError {
			obj["isError"] = true
		}
		out(obj)
	case agent.ToolExecutionStart:
		out(map[string]any{
			"type":       "tool_execution_start",
			"toolCallId": e.ToolCallID,
			"tool":       e.ToolName,
			"arguments":  string(e.Arguments),
		})
	case agent.ToolExecutionUpdate:
		out(map[string]any{
			"type":       "tool_execution_update",
			"toolCallId": e.ToolCallID,
			"tool":       e.ToolName,
			"partial":    blocksText(e.Update.Content),
		})
	case agent.ToolExecutionEnd:
		obj := map[string]any{
			"type":       "tool_execution_end",
			"toolCallId": e.ToolCallID,
			"tool":       e.ToolName,
			"isError":    e.IsError,
			"output":     blocksText(e.Result.Content),
		}
		out(obj)
	}
}

func messageText(m llm.Message) string { return blocksText(m.Content) }

func blocksText(blocks []llm.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if tb, ok := b.(llm.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}
