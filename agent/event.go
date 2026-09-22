package agent

import (
	"encoding/json"

	"github.com/daqing/motionloop/llm"
)

// Event is the observation surface of the agent loop. The full set is
// defined up front — agent and turn lifecycle, message lifecycle, and tool
// execution lifecycle — mirroring pi's event sequence; hooks arrive in
// Phase 3.
type Event interface {
	isEvent()
}

var _ []Event = []Event{
	AgentStart{}, AgentEnd{},
	TurnStart{}, TurnEnd{},
	MessageStart{}, MessageUpdate{}, MessageEnd{},
	ToolExecutionStart{}, ToolExecutionUpdate{}, ToolExecutionEnd{},
}

// AgentStart opens a run.
type AgentStart struct{}

// AgentEnd closes a run; no further events follow it.
type AgentEnd struct {
	Messages []llm.Message
}

// TurnStart opens one LLM call plus its tool executions.
type TurnStart struct{}

// TurnEnd closes one turn.
type TurnEnd struct {
	Message     llm.Message
	ToolResults []llm.Message
}

// MessageStart opens a message.
type MessageStart struct {
	Message llm.Message
}

// MessageUpdate streams an assistant delta.
type MessageUpdate struct {
	Message llm.Message
	Delta   llm.StreamEvent
}

// MessageEnd closes a message.
type MessageEnd struct {
	Message llm.Message
}

// ToolExecutionStart opens one tool execution.
type ToolExecutionStart struct {
	ToolCallID string
	ToolName   string
	Arguments  json.RawMessage
}

// ToolExecutionUpdate streams partial tool output.
type ToolExecutionUpdate struct {
	ToolCallID string
	ToolName   string
	Update     Update
}

// ToolExecutionEnd closes one tool execution.
type ToolExecutionEnd struct {
	ToolCallID string
	ToolName   string
	Result     Result
	IsError    bool
}

func (AgentStart) isEvent()          {}
func (AgentEnd) isEvent()            {}
func (TurnStart) isEvent()           {}
func (TurnEnd) isEvent()             {}
func (MessageStart) isEvent()        {}
func (MessageUpdate) isEvent()       {}
func (MessageEnd) isEvent()          {}
func (ToolExecutionStart) isEvent()  {}
func (ToolExecutionUpdate) isEvent() {}
func (ToolExecutionEnd) isEvent()    {}
