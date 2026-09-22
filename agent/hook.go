package agent

import (
	"context"

	"github.com/daqing/motionloop/llm"
)

// Hook contract, aligned with pi: hooks must not panic and must not block
// longer than the context allows. Zero-value returns mean "do not
// intervene".

// BeforeToolCallResult is the verdict of a BeforeToolCall hook.
type BeforeToolCallResult struct {
	// Block prevents execution; the loop emits an error-flagged tool
	// result carrying Reason instead of running the tool.
	Block bool
	// Reason is the text of the blocked result; empty falls back to a
	// default message.
	Reason string
	// Terminate marks the blocked result as terminating.
	Terminate bool
}

// BeforeToolCall runs after tool_execution_start and argument validation
// and may block each call.
type BeforeToolCall func(ctx context.Context, call ToolCall) BeforeToolCallResult

// AfterToolCallResult overrides selected fields of an executed tool
// result. Nil fields keep the executed values.
type AfterToolCallResult struct {
	Content   []llm.ContentBlock
	Details   any
	IsError   *bool
	Terminate *bool
}

// AfterToolCall runs after tool execution finishes and before
// tool_execution_end is emitted.
type AfterToolCall func(ctx context.Context, call ToolCall, result Result, isError bool) AfterToolCallResult

// TurnDecision controls run scheduling after one completed turn.
type TurnDecision int

const (
	// DecisionDefault keeps normal scheduling: continue when tool results
	// exist, end otherwise.
	DecisionDefault TurnDecision = iota
	// DecisionEnd stops the run after turn_end without another request.
	DecisionEnd
	// DecisionContinue ensures one more provider request even without tool
	// results. Returning it unconditionally creates an endless loop.
	DecisionContinue
)

// Turn is one completed assistant turn plus its tool results.
type Turn struct {
	Message     llm.Message
	ToolResults []llm.Message
}

// FinishTurn runs after the assistant message and all tool results are
// finalized, immediately before turn_end. It does not run for error or
// aborted turns, which are hard exits.
type FinishTurn func(ctx context.Context, turn Turn) TurnDecision

// TransformContext prunes or injects transcript content before every LLM
// request — the compaction and session-replay integration point. The
// transcript itself is never modified.
type TransformContext func(ctx context.Context, messages []llm.Message) []llm.Message

// ConvertToLLM filters or converts messages right before the provider
// request, after TransformContext. The default passthrough keeps every
// message; future custom message types drop or map their entries here.
type ConvertToLLM func(messages []llm.Message) []llm.Message
