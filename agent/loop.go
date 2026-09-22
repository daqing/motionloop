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

// ErrRunInProgress is returned by Prompt and Continue when another run has
// not settled. Steer and FollowUp stay available while a run is in flight.
var ErrRunInProgress = errors.New("agent: a run is already in progress")

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

// WithBeforeToolCall installs a per-call gate that may block executions.
func WithBeforeToolCall(hook BeforeToolCall) Option {
	return func(a *Agent) { a.beforeToolCall = hook }
}

// WithAfterToolCall installs a post-execution result rewriter.
func WithAfterToolCall(hook AfterToolCall) Option {
	return func(a *Agent) { a.afterToolCall = hook }
}

// WithFinishTurn installs a per-turn scheduling decision hook.
func WithFinishTurn(hook FinishTurn) Option {
	return func(a *Agent) { a.finishTurn = hook }
}

// WithTransformContext installs a transcript transform applied before
// every provider request.
func WithTransformContext(hook TransformContext) Option {
	return func(a *Agent) { a.transformContext = hook }
}

// WithConvertToLLM installs the final message filter applied after
// TransformContext.
func WithConvertToLLM(hook ConvertToLLM) Option {
	return func(a *Agent) { a.convertToLLM = hook }
}

// Agent runs the LLM-to-tool loop against one provider and model. Each run
// streams one assistant response per turn, executes requested tool calls
// (parallel by default, in assistant source order for persistence), and
// continues until the assistant stops calling tools, every result in a
// batch asks to terminate, the stream fails, or the context is canceled.
type Agent struct {
	provider     llm.Provider
	model        llm.Model
	streamOpts   llm.StreamOptions
	thinking     llm.ThinkingLevel
	tools        []Tool
	systemPrompt string
	systemMsg    *llm.Message

	beforeToolCall   BeforeToolCall
	afterToolCall    AfterToolCall
	finishTurn       FinishTurn
	transformContext TransformContext
	convertToLLM     ConvertToLLM

	mu       sync.Mutex
	messages []llm.Message
	subs     []func(Event)
	toolset  map[string]Tool
	steerQ   []llm.Message
	followQ  []llm.Message
	running  bool
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

// Steer queues a user message for injection at the next turn boundary.
// One-at-a-time: only the oldest queued message drains per boundary.
func (a *Agent) Steer(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.steerQ = append(a.steerQ, llm.Message{
		Role:      llm.RoleUser,
		Content:   []llm.ContentBlock{llm.TextBlock{Text: text}},
		Timestamp: now(),
	})
}

// FollowUp queues a user message that Continue consumes when a run has
// naturally stopped on an assistant tail.
func (a *Agent) FollowUp(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.followQ = append(a.followQ, llm.Message{
		Role:      llm.RoleUser,
		Content:   []llm.ContentBlock{llm.TextBlock{Text: text}},
		Timestamp: now(),
	})
}

// Continue starts a run from the existing transcript. A user or toolResult
// tail retries directly; an assistant tail first consumes one queued
// steering message, then one queued follow-up.
func (a *Agent) Continue(ctx context.Context) error {
	if !a.beginRun() {
		return ErrRunInProgress
	}
	defer a.endRun()

	a.mu.Lock()
	n := len(a.messages)
	tailIsAssistant := n > 0 && a.messages[n-1].Role == llm.RoleAssistant
	onlySystem := n == 0 || (n == 1 && a.messages[0].Role == llm.RoleSystem)
	a.mu.Unlock()
	if onlySystem {
		return errors.New("agent: nothing to continue")
	}

	var pending *llm.Message
	if m := a.drainSteering(); m != nil {
		a.appendMessage(*m)
		pending = m
	} else if tailIsAssistant {
		if m := a.drainFollowUp(); m != nil {
			a.appendMessage(*m)
			pending = m
		} else {
			return errors.New("agent: continuing after an assistant tail requires queued steering or follow-up input")
		}
	}
	return a.loop(ctx, pending)
}

// Prompt drives one run.
func (a *Agent) Prompt(ctx context.Context, input string) error {
	if !a.beginRun() {
		return ErrRunInProgress
	}
	defer a.endRun()

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
	return a.loop(ctx, &userMsg)
}

func (a *Agent) loop(ctx context.Context, firstPending *llm.Message) error {
	a.emit(AgentStart{})
	defer func() { a.emit(AgentEnd{Messages: a.Messages()}) }()

	first := true
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var pending *llm.Message
		if first {
			pending = firstPending
		} else if m := a.drainSteering(); m != nil {
			a.appendMessage(*m)
			pending = m
		}
		a.emit(TurnStart{})
		if first && a.systemMsg != nil {
			a.mu.Lock()
			sys := *a.systemMsg
			a.systemMsg = nil
			a.mu.Unlock()
			a.emit(MessageStart{Message: sys})
			a.emit(MessageEnd{Message: sys})
		}
		if pending != nil {
			a.emit(MessageStart{Message: *pending})
			a.emit(MessageEnd{Message: *pending})
		}
		first = false

		assistant, err := a.streamAssistant(ctx)
		if err != nil {
			a.emit(TurnEnd{Message: assistant})
			return err
		}

		results, allTerminate := a.executeTools(ctx, assistant)

		decision := DecisionDefault
		if a.finishTurn != nil {
			decision = a.finishTurn(ctx, Turn{Message: assistant, ToolResults: results})
		}
		a.emit(TurnEnd{Message: assistant, ToolResults: results})
		switch {
		case allTerminate || decision == DecisionEnd:
			return nil
		case decision == DecisionContinue:
			continue
		default:
			if len(results) == 0 {
				return nil
			}
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
	if a.transformContext != nil {
		msgs = a.transformContext(ctx, msgs)
	}
	if a.convertToLLM != nil {
		msgs = a.convertToLLM(msgs)
	}

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

// outcome is one finalized tool result, in source order when indexed.
type outcome struct {
	res   Result
	isErr bool
}

func (a *Agent) executeTools(ctx context.Context, assistant llm.Message) ([]llm.Message, bool) {
	var calls []llm.ToolCallBlock
	for _, b := range assistant.Content {
		if tc, ok := b.(llm.ToolCallBlock); ok {
			calls = append(calls, tc)
		}
	}
	if len(calls) == 0 {
		return nil, false
	}

	type prepared struct {
		idx  int
		tc   llm.ToolCallBlock
		tool Tool
		call ToolCall
		done *outcome
	}

	batchSequential := false
	preflight := make([]prepared, 0, len(calls))
	for i, tc := range calls {
		a.emit(ToolExecutionStart{ToolCallID: tc.ID, ToolName: tc.Name, Arguments: tc.Arguments})
		p := prepared{idx: i, tc: tc}

		tool, ok := a.lookupTool(tc.Name)
		if !ok {
			p.done = a.finalizePreflight(tc, ErrorResult(fmt.Errorf("unknown tool %q", tc.Name)))
			preflight = append(preflight, p)
			continue
		}
		if err := Validate(tool.Parameters(), tc.Arguments); err != nil {
			p.done = a.finalizePreflight(tc, ErrorResult(fmt.Errorf("invalid arguments: %v", err)))
			preflight = append(preflight, p)
			continue
		}
		call := ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
		if a.beforeToolCall != nil {
			verdict := a.beforeToolCall(ctx, call)
			if verdict.Block {
				reason := verdict.Reason
				if reason == "" {
					reason = "tool call blocked"
				}
				p.done = a.finalizePreflight(tc, Result{
					Content:   []llm.ContentBlock{llm.TextBlock{Text: reason}},
					Terminate: verdict.Terminate,
				})
				preflight = append(preflight, p)
				continue
			}
		}
		if modeOf(tool) == ExecutionSequential {
			batchSequential = true
		}
		p.tool = tool
		p.call = call
		preflight = append(preflight, p)
	}

	outcomes := make([]outcome, len(calls))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range preflight {
		if p.done != nil {
			outcomes[p.idx] = *p.done
			continue
		}
		if batchSequential {
			outcomes[p.idx] = a.runExecuted(ctx, p.tc, p.tool, p.call)
			continue
		}
		wg.Add(1)
		go func(p prepared) {
			defer wg.Done()
			out := a.runExecuted(ctx, p.tc, p.tool, p.call)
			mu.Lock()
			outcomes[p.idx] = out
			mu.Unlock()
		}(p)
	}
	wg.Wait()

	results := make([]llm.Message, 0, len(calls))
	allTerminate := true
	for i, out := range outcomes {
		tc := calls[i]
		msg := llm.Message{
			Role:       llm.RoleToolResult,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Content:    out.res.Content,
			IsError:    out.isErr,
			Timestamp:  now(),
		}
		a.emit(MessageStart{Message: msg})
		a.appendMessage(msg)
		a.emit(MessageEnd{Message: msg})
		results = append(results, msg)
		if !out.res.Terminate {
			allTerminate = false
		}
	}
	return results, allTerminate && len(results) > 0
}

// finalizePreflight closes a call that never executes (unknown tool,
// invalid arguments, blocked by BeforeToolCall) with an error-flagged
// result.
func (a *Agent) finalizePreflight(tc llm.ToolCallBlock, res Result) *outcome {
	a.emit(ToolExecutionEnd{ToolCallID: tc.ID, ToolName: tc.Name, Result: res, IsError: true})
	return &outcome{res: res, isErr: true}
}

// runExecuted performs one tool execution with the AfterToolCall override
// applied, emitting updates and the completion-order ToolExecutionEnd.
func (a *Agent) runExecuted(ctx context.Context, tc llm.ToolCallBlock, tool Tool, call ToolCall) outcome {
	res, err := tool.Execute(ctx, call, func(u Update) {
		a.emit(ToolExecutionUpdate{ToolCallID: tc.ID, ToolName: tc.Name, Update: u})
	})
	isErr := err != nil
	if isErr {
		res = ErrorResult(err)
	}
	if a.afterToolCall != nil {
		override := a.afterToolCall(ctx, call, res, isErr)
		if override.Content != nil {
			res.Content = override.Content
		}
		if override.Details != nil {
			res.Details = override.Details
		}
		if override.IsError != nil {
			isErr = *override.IsError
		}
		if override.Terminate != nil {
			res.Terminate = *override.Terminate
		}
	}
	a.emit(ToolExecutionEnd{ToolCallID: tc.ID, ToolName: tc.Name, Result: res, IsError: isErr})
	return outcome{res: res, isErr: isErr}
}

func (a *Agent) lookupTool(name string) (Tool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.toolset[name]
	return t, ok
}

func (a *Agent) drainSteering() *llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.steerQ) == 0 {
		return nil
	}
	m := a.steerQ[0]
	a.steerQ = a.steerQ[1:]
	return &m
}

func (a *Agent) drainFollowUp() *llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.followQ) == 0 {
		return nil
	}
	m := a.followQ[0]
	a.followQ = a.followQ[1:]
	return &m
}

func (a *Agent) beginRun() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return false
	}
	a.running = true
	return true
}

func (a *Agent) endRun() {
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
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
