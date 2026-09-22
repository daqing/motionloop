package agent

import (
	"context"
	"encoding/json"

	"github.com/daqing/motionloop/llm"
)

// ExecutionMode selects how one tool's calls are scheduled within a batch.
type ExecutionMode int

const (
	// ExecutionParallel runs concurrently with other tool calls. It is the
	// default for every tool.
	ExecutionParallel ExecutionMode = iota
	// ExecutionSequential must not run concurrently with anything. Any
	// batch containing a sequential call degrades to fully sequential
	// execution.
	ExecutionSequential
)

// executionModeAware is the optional interface a Tool implements to force
// sequential scheduling; tools that do not implement it run parallel.
type executionModeAware interface {
	ExecutionMode() ExecutionMode
}

// modeOf reports a tool's execution mode, parallel by default.
func modeOf(t Tool) ExecutionMode {
	if m, ok := t.(executionModeAware); ok {
		return m.ExecutionMode()
	}
	return ExecutionParallel
}

// Tool is the extension atom of the framework. Implementations declare
// their arguments as a struct with `json` and `jsonschema` tags and pass it
// to SchemaFor to produce Parameters.
type Tool interface {
	Name() string
	Description() string
	Parameters() *Schema
	// Execute runs one call. Returning a non-nil error produces an
	// error-flagged tool result; it does not abort the run. Emit streams
	// partial results as ToolExecutionUpdate events.
	Execute(ctx context.Context, call ToolCall, emit func(Update)) (Result, error)
}

// ToolCall is one tool invocation requested by the assistant.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Result is a tool execution outcome. Content goes back to the model;
// Details is structured data for UIs and logs and never enters the LLM
// context. Terminate hints that the run should stop after this batch when
// every result in the batch sets it.
type Result struct {
	Content   []llm.ContentBlock
	Details   any
	Terminate bool
}

// Update is a partial result streamed during execution.
type Update struct {
	Content []llm.ContentBlock
	Details any
}

// ErrorResult builds the result used when a tool fails: the error message
// becomes the content of an error-flagged tool result message.
func ErrorResult(err error) Result {
	return Result{Content: []llm.ContentBlock{llm.TextBlock{Text: err.Error()}}}
}
