package agent

import (
	"context"
	"sync"

	"github.com/daqing/motionloop/llm"
)

// FakeCall records one request received by a FakeProvider.
type FakeCall struct {
	Messages []llm.Message
	Options  llm.StreamOptions
}

// FakeProvider is a scripted llm.Provider for tests: each Stream call
// consumes the next scripted step of stream events. Requests beyond the
// script fail with an error stop.
type FakeProvider struct {
	mu    sync.Mutex
	steps [][]llm.StreamEvent
	calls []FakeCall
}

// NewFakeProvider scripts one step per expected request.
func NewFakeProvider(steps ...[]llm.StreamEvent) *FakeProvider {
	return &FakeProvider{steps: steps}
}

// ID implements llm.Provider.
func (f *FakeProvider) ID() string { return "fake" }

// Capabilities implements llm.Provider.
func (f *FakeProvider) Capabilities(llm.Model) llm.Capabilities {
	return llm.Capabilities{ParallelToolCalls: true}
}

// Stream implements llm.Provider.
func (f *FakeProvider) Stream(ctx context.Context, model llm.Model, messages []llm.Message, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	var step []llm.StreamEvent
	if len(f.steps) > 0 {
		step, f.steps = f.steps[0], f.steps[1:]
	}
	f.calls = append(f.calls, FakeCall{Messages: append([]llm.Message(nil), messages...), Options: opts})
	f.mu.Unlock()

	ch := make(chan llm.StreamEvent, len(step)+1)
	go func() {
		defer close(ch)
		if step == nil {
			ch <- llm.Stop{Reason: llm.StopReasonError, ErrorMessage: "fake provider: no scripted steps left"}
			return
		}
		for _, ev := range step {
			ch <- ev
		}
	}()
	return ch, nil
}

// Calls returns the recorded requests.
func (f *FakeProvider) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeCall(nil), f.calls...)
}

// FakeTextEvents scripts one plain text response.
func FakeTextEvents(text string) []llm.StreamEvent {
	return []llm.StreamEvent{
		llm.TextDelta{Delta: text},
		llm.Stop{Reason: llm.StopReasonStop},
	}
}

// FakeToolCallEvents scripts one response requesting a tool call.
func FakeToolCallEvents(id, name, args string) []llm.StreamEvent {
	return []llm.StreamEvent{
		llm.TextDelta{Delta: "let me check"},
		llm.ToolCallDelta{Index: 0, ID: id, Name: name},
		llm.ToolCallDelta{Index: 0, ArgumentsDelta: args},
		llm.Stop{Reason: llm.StopReasonToolUse},
	}
}

// FakeErrorEvents scripts one failed response.
func FakeErrorEvents(msg string) []llm.StreamEvent {
	return []llm.StreamEvent{
		llm.Stop{Reason: llm.StopReasonError, ErrorMessage: msg},
	}
}
