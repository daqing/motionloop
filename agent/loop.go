package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/daqing/motionloop/llm"
)

// ErrStreamFailed wraps LLM-side stream failures; the run ends with a
// complete event sequence when it is returned.
var ErrStreamFailed = errors.New("llm stream failed")

// ErrAborted wraps provider-side aborts that are not context cancellations.
var ErrAborted = errors.New("run aborted")

// Option configures an Agent.
type Option func(*Agent)

// WithTools sets the executable tool loadout.
func WithTools(tools ...Tool) Option {
	return func(a *Agent) { a.tools = append(a.tools, tools...) }
}

// WithSystemPrompt prepends a system message on the first prompt when the
// transcript is empty.
func WithSystemPrompt(prompt string) Option {
	return func(a *Agent) { a.systemPrompt = prompt }
}

// WithMessages seeds the transcript.
func WithMessages(msgs ...llm.Message) Option {
	return func(a *Agent) { a.messages = append(a.messages, msgs...) }
}

// WithStreamOptions sets base provider options (API key, base URL, ...).
func WithStreamOptions(opts llm.StreamOptions) Option {
	return func(a *Agent) { a.streamOpts = opts }
}

// WithThinkingLevel selects the reasoning tier for provider requests.
func WithThinkingLevel(level llm.ThinkingLevel) Option {
	return func(a *Agent) { a.thinking = level }
}

// Agent runs the LLM-to-tool loop against one provider and model. A Prompt
// call streams one assistant response, executes requested tools in
// assistant source order, and continues until the assistant stops calling
// tools, every result in a batch asks to terminate, the stream fails, or
// the context is canceled.
type Agent struct {
	provider     llm.Provider
	model        llm.Model
	streamOpts   llm.StreamOptions
	thinking     llm.ThinkingLevel
	tools        []Tool
	systemPrompt string
	systemMsg    *llm.Message

	mu       sync.Mutex
	messages []llm.Message
	subs     []func(Event)
	toolset  map[string]Tool
}

// New creates an Agent.
func New(provider llm.Provider, model llm.Model, opts ...Option) *Agent {
	a := &Agent{provider: provider, model: model, toolset: map[string]Tool{}}
	for _, o := range opts {
		o(a)
	}
	for _, t := range a.tools {
		a.toolset[t.Name()] = t
	}
	return a
}

// Subscribe registers a synchronous event listener; listeners run in
// registration order.
func (a *Agent) Subscribe(fn func(Event)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.subs = append(a.subs, fn)
}

// Messages returns a copy of the transcript.
func (a *Agent) Messages() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]llm.Message(nil), a.messages...)
}

// Prompt drives one run.
func (a *Agent) Prompt(ctx context.Context, input string) error {
	a.mu.Lock()
	if len(a.messages) == 0 && a.systemPrompt != "" {
		msg := llm.Message{
			Role:      llm.RoleSystem,
			Content:   []llm.ContentBlock{llm.TextBlock{Text: a.systemPrompt}},
			Timestamp: now(),
		}
		a.messages = append(a.messages, msg)
		a.systemMsg = &msg
	}
	a.mu.Unlock()

	userMsg := llm.Message{
		Role:      llm.RoleUser,
		Content:   []llm.ContentBlock{llm.TextBlock{Text: input}},
		Timestamp: now(),
	}
	a.appendMessage(userMsg)

	a.emit(AgentStart{})
	defer a.emit(AgentEnd{Messages: a.Messages()})

	first := true
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.emit(TurnStart{})
		if first {
			first = false
			if a.systemMsg != nil {
				a.emit(MessageStart{Message: *a.systemMsg})
				a.emit(MessageEnd{Message: *a.systemMsg})
			}
			a.emit(MessageStart{Message: userMsg})
			a.emit(MessageEnd{Message: userMsg})
		}

		assistant, err := a.streamAssistant(ctx)
		if err != nil {
			a.emit(TurnEnd{Message: assistant})
			return err
		}

		results, terminate := a.executeTools(ctx, assistant)
		a.emit(TurnEnd{Message: assistant, ToolResults: results})
		if len(results) == 0 || terminate {
			return nil
		}
	}
}

func (a *Agent) streamAssistant(ctx context.Context) (llm.Message, error) {
	a.mu.Lock()
	msgs := append([]llm.Message(nil), a.messages...)
	opts := a.streamOpts
	opts.ThinkingLevel = a.thinking
	opts.Tools = a.toolDecls()
	a.mu.Unlock()

	assistant := llm.Message{
		Role:      llm.RoleAssistant,
		Provider:  a.model.ProviderID,
		Model:     a.model.ModelID,
		Timestamp: now(),
	}
	a.emit(MessageStart{Message: assistant})

	var think, text strings.Builder
	calls := map[int]*callAccumulator{}
	var order []*callAccumulator
	var stop llm.Stop

	ch, err := a.provider.Stream(ctx, a.model, msgs, opts)
	if err != nil {
		return assistant, err
	}
	for ev := range ch {
		switch e := ev.(type) {
		case llm.TextDelta:
			text.WriteString(e.Delta)
			a.emitPartial(assistant, &think, &text, calls, order, e)
		case llm.ThinkingDelta:
			think.WriteString(e.Delta)
			a.emitPartial(assistant, &think, &text, calls, order, e)
		case llm.ToolCallDelta:
			acc := calls[e.Index]
			if acc == nil {
				acc = &callAccumulator{}
				calls[e.Index] = acc
				order = append(order, acc)
			}
			if e.ID != "" {
				acc.id = e.ID
			}
			if e.Name != "" {
				acc.name = e.Name
			}
			acc.args.WriteString(e.ArgumentsDelta)
			a.emitPartial(assistant, &think, &text, calls, order, e)
		case llm.UsageUpdate:
			u := e.Usage
			assistant.Usage = &u
		case llm.Stop:
			stop = e
		}
	}

	assistant.Content = contentBlocks(&think, &text, order)
	assistant.StopReason = stop.Reason
	assistant.ErrorMessage = stop.ErrorMessage
	a.appendMessage(assistant)
	a.emit(MessageEnd{Message: assistant})

	switch stop.Reason {
	case llm.StopReasonError:
		return assistant, fmt.Errorf("%w: %s", ErrStreamFailed, stop.ErrorMessage)
	case llm.StopReasonAborted:
		if err := ctx.Err(); err != nil {
			return assistant, err
		}
		return assistant, fmt.Errorf("%w: %s", ErrAborted, stop.ErrorMessage)
	}
	return assistant, nil
}

func (a *Agent) executeTools(ctx context.Context, assistant llm.Message) ([]llm.Message, bool) {
	var results []llm.Message
	allTerminate := true
	for _, b := range assistant.Content {
		tc, ok := b.(llm.ToolCallBlock)
		if !ok {
			continue
		}
		res, isErr := a.runTool(ctx, tc)
		if !res.Terminate {
			allTerminate = false
		}
		msg := llm.Message{
			Role:       llm.RoleToolResult,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Content:    res.Content,
			IsError:    isErr,
			Timestamp:  now(),
		}
		a.emit(MessageStart{Message: msg})
		a.appendMessage(msg)
		a.emit(MessageEnd{Message: msg})
		results = append(results, msg)
	}
	return results, allTerminate && len(results) > 0
}

func (a *Agent) runTool(ctx context.Context, tc llm.ToolCallBlock) (Result, bool) {
	a.mu.Lock()
	tool, ok := a.toolset[tc.Name]
	a.mu.Unlock()
	if !ok {
		res := ErrorResult(fmt.Errorf("unknown tool %q", tc.Name))
		a.emit(ToolExecutionStart{ToolCallID: tc.ID, ToolName: tc.Name, Arguments: tc.Arguments})
		a.emit(ToolExecutionEnd{ToolCallID: tc.ID, ToolName: tc.Name, Result: res, IsError: true})
		return res, true
	}

	a.emit(ToolExecutionStart{ToolCallID: tc.ID, ToolName: tc.Name, Arguments: tc.Arguments})
	if err := Validate(tool.Parameters(), tc.Arguments); err != nil {
		res := ErrorResult(fmt.Errorf("invalid arguments: %v", err))
		a.emit(ToolExecutionEnd{ToolCallID: tc.ID, ToolName: tc.Name, Result: res, IsError: true})
		return res, true
	}

	call := ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
	res, err := tool.Execute(ctx, call, func(u Update) {
		a.emit(ToolExecutionUpdate{ToolCallID: tc.ID, ToolName: tc.Name, Update: u})
	})
	isErr := err != nil
	if isErr {
		res = ErrorResult(err)
	}
	a.emit(ToolExecutionEnd{ToolCallID: tc.ID, ToolName: tc.Name, Result: res, IsError: isErr})
	return res, isErr
}

func (a *Agent) toolDecls() []llm.ToolDecl {
	decls := make([]llm.ToolDecl, 0, len(a.tools))
	for _, t := range a.tools {
		raw, err := json.Marshal(t.Parameters())
		if err != nil {
			raw = nil
		}
		decls = append(decls, llm.ToolDecl{Name: t.Name(), Description: t.Description(), Parameters: raw})
	}
	return decls
}

func (a *Agent) appendMessage(msg llm.Message) {
	a.mu.Lock()
	a.messages = append(a.messages, msg)
	a.mu.Unlock()
}

func (a *Agent) emit(ev Event) {
	a.mu.Lock()
	var subs []func(Event)
	subs = append(subs, a.subs...)
	a.mu.Unlock()
	for _, fn := range subs {
		fn(ev)
	}
}

func (a *Agent) emitPartial(base llm.Message, think, text *strings.Builder, calls map[int]*callAccumulator, order []*callAccumulator, delta llm.StreamEvent) {
	base.Content = contentBlocks(think, text, order)
	a.emit(MessageUpdate{Message: base, Delta: delta})
}

func contentBlocks(think, text *strings.Builder, order []*callAccumulator) []llm.ContentBlock {
	var blocks []llm.ContentBlock
	if think.Len() > 0 {
		blocks = append(blocks, llm.ThinkingBlock{Thinking: think.String()})
	}
	if text.Len() > 0 {
		blocks = append(blocks, llm.TextBlock{Text: text.String()})
	}
	for _, acc := range order {
		blocks = append(blocks, llm.ToolCallBlock{
			ID:        acc.id,
			Name:      acc.name,
			Arguments: json.RawMessage(acc.args.String()),
		})
	}
	return blocks
}

type callAccumulator struct {
	id, name string
	args     strings.Builder
}

func now() int64 { return time.Now().UnixMilli() }
