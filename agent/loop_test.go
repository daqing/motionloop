package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daqing/motionloop/llm"
)

type echoTool struct {
	mu    sync.Mutex
	execs int
	fail  bool
}

type echoParams struct {
	Text string `json:"text" jsonschema:"required,description=Text to echo"`
}

func (e *echoTool) Name() string { return "echo" }

func (e *echoTool) Description() string { return "Echo text back" }

func (e *echoTool) Parameters() *Schema { return MustSchemaFor(&echoParams{}) }

func (e *echoTool) Execute(ctx context.Context, call ToolCall, emit func(Update)) (Result, error) {
	e.mu.Lock()
	e.execs++
	e.mu.Unlock()
	if e.fail {
		return Result{}, errors.New("boom")
	}
	var p echoParams
	if err := json.Unmarshal(call.Arguments, &p); err != nil {
		return Result{}, err
	}
	return Result{
		Content:   []llm.ContentBlock{llm.TextBlock{Text: "echo: " + p.Text}},
		Details:   map[string]any{"length": len(p.Text)},
		Terminate: false,
	}, nil
}

func (e *echoTool) executions() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.execs
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) record(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	kinds := make([]string, len(r.events))
	for i, ev := range r.events {
		kinds[i] = kind(ev)
	}
	return kinds
}

func kind(ev Event) string {
	switch ev.(type) {
	case AgentStart:
		return "AgentStart"
	case AgentEnd:
		return "AgentEnd"
	case TurnStart:
		return "TurnStart"
	case TurnEnd:
		return "TurnEnd"
	case MessageStart:
		return "MessageStart"
	case MessageUpdate:
		return "MessageUpdate"
	case MessageEnd:
		return "MessageEnd"
	case ToolExecutionStart:
		return "ToolExecutionStart"
	case ToolExecutionUpdate:
		return "ToolExecutionUpdate"
	case ToolExecutionEnd:
		return "ToolExecutionEnd"
	default:
		return "unknown"
	}
}

func equalKinds(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func newTestAgent(provider llm.Provider, tools ...Tool) (*Agent, *recorder) {
	rec := &recorder{}
	a := New(provider, llm.Model{ProviderID: "fake", ModelID: "test"}, WithTools(tools...))
	a.Subscribe(rec.record)
	return a, rec
}

func TestPromptPlainText(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("hello there"))
	a, rec := newTestAgent(fake)

	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	want := []string{
		"AgentStart", "TurnStart", "MessageStart", "MessageEnd",
		"MessageStart", "MessageUpdate", "MessageEnd", "TurnEnd", "AgentEnd",
	}
	if got := rec.kinds(); !equalKinds(got, want) {
		t.Fatalf("event kinds:\nwant %v\ngot  %v", want, got)
	}

	msgs := a.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2 (user, assistant)", len(msgs))
	}
	if msgs[0].Role != llm.RoleUser || msgs[1].Role != llm.RoleAssistant {
		t.Fatalf("roles = %v, %v", msgs[0].Role, msgs[1].Role)
	}
	if len(fake.Calls()) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(fake.Calls()))
	}
}

func TestPromptSystemMessageEmitted(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("ok"))
	rec := &recorder{}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"}, WithSystemPrompt("be brief"))
	a.Subscribe(rec.record)

	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	want := []string{
		"AgentStart", "TurnStart",
		"MessageStart", "MessageEnd",
		"MessageStart", "MessageEnd",
		"MessageStart", "MessageUpdate", "MessageEnd",
		"TurnEnd", "AgentEnd",
	}
	if got := rec.kinds(); !equalKinds(got, want) {
		t.Fatalf("event kinds:\nwant %v\ngot  %v", want, got)
	}
	msgs := a.Messages()
	if len(msgs) != 3 || msgs[0].Role != llm.RoleSystem {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestPromptSingleTool(t *testing.T) {
	echo := &echoTool{}
	fake := NewFakeProvider(
		FakeToolCallEvents("call_1", "echo", `{"text":"world"}`),
		FakeTextEvents("all done"),
	)
	a, rec := newTestAgent(fake, echo)

	if err := a.Prompt(context.Background(), "say world"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	want := []string{
		"AgentStart", "TurnStart", "MessageStart", "MessageEnd",
		"MessageStart", "MessageUpdate", "MessageUpdate", "MessageUpdate", "MessageEnd",
		"ToolExecutionStart", "ToolExecutionEnd", "MessageStart", "MessageEnd",
		"TurnEnd",
		"TurnStart", "MessageStart", "MessageUpdate", "MessageEnd", "TurnEnd",
		"AgentEnd",
	}
	if got := rec.kinds(); !equalKinds(got, want) {
		t.Fatalf("event kinds:\nwant %v\ngot  %v", want, got)
	}

	msgs := a.Messages()
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want 4 (user, assistant, toolResult, assistant)", len(msgs))
	}
	tr := msgs[2]
	if tr.Role != llm.RoleToolResult || tr.ToolCallID != "call_1" || tr.ToolName != "echo" || tr.IsError {
		t.Fatalf("toolResult = %+v", tr)
	}
	if got := textOf(tr.Content); got != "echo: world" {
		t.Fatalf("toolResult content = %q", got)
	}
	if echo.executions() != 1 {
		t.Fatalf("executions = %d, want 1", echo.executions())
	}
	if len(fake.Calls()) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(fake.Calls()))
	}
	if got := textOf(fake.Calls()[1].Messages[2].Content); got != "echo: world" {
		t.Fatalf("second request toolResult content = %q", got)
	}
	if len(fake.Calls()[1].Options.Tools) != 1 || fake.Calls()[1].Options.Tools[0].Name != "echo" {
		t.Fatalf("tools not declared in request: %+v", fake.Calls()[1].Options.Tools)
	}
}

func TestPromptToolErrorContinues(t *testing.T) {
	echo := &echoTool{fail: true}
	fake := NewFakeProvider(
		FakeToolCallEvents("call_1", "echo", `{"text":"x"}`),
		FakeTextEvents("recovered"),
	)
	a, rec := newTestAgent(fake, echo)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	msgs := a.Messages()
	tr := msgs[2]
	if !tr.IsError || textOf(tr.Content) != "boom" {
		t.Fatalf("toolResult = %+v, want isError with boom", tr)
	}
	if got := textOf(msgs[3].Content); got != "recovered" {
		t.Fatalf("final assistant = %q", got)
	}

	var toolEnd ToolExecutionEnd
	for _, ev := range rec.events {
		if te, ok := ev.(ToolExecutionEnd); ok {
			toolEnd = te
		}
	}
	if !toolEnd.IsError {
		t.Fatalf("ToolExecutionEnd.IsError = false")
	}
}

func TestPromptLLMErrorEndsRun(t *testing.T) {
	fake := NewFakeProvider(FakeErrorEvents("rate limited"))
	a, rec := newTestAgent(fake)

	err := a.Prompt(context.Background(), "hi")
	if err == nil || !errors.Is(err, ErrStreamFailed) || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err = %v, want ErrStreamFailed wrapping rate limited", err)
	}
	msgs := a.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	assistant := msgs[1]
	if assistant.StopReason != llm.StopReasonError || assistant.ErrorMessage != "rate limited" {
		t.Fatalf("assistant = %+v", assistant)
	}
	kinds := rec.kinds()
	if kinds[len(kinds)-1] != "AgentEnd" {
		t.Fatalf("last event = %v, want AgentEnd", kinds[len(kinds)-1])
	}
}

type blockingProvider struct{}

func (blockingProvider) ID() string { return "blocking" }

func (blockingProvider) Capabilities(llm.Model) llm.Capabilities { return llm.Capabilities{} }

func (blockingProvider) Stream(ctx context.Context, _ llm.Model, _ []llm.Message, _ llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch := make(chan llm.StreamEvent, 1)
	go func() {
		defer close(ch)
		<-ctx.Done()
		ch <- llm.Stop{Reason: llm.StopReasonAborted, ErrorMessage: "canceled"}
	}()
	return ch, nil
}

func TestPromptContextCanceledDuringStream(t *testing.T) {
	rec := &recorder{}
	a := New(blockingProvider{}, llm.Model{ProviderID: "blocking", ModelID: "m"})
	a.Subscribe(rec.record)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := a.Prompt(ctx, "hi")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	kinds := rec.kinds()
	if kinds[len(kinds)-1] != "AgentEnd" {
		t.Fatalf("last event = %v, want AgentEnd (event sequence must stay complete)", kinds[len(kinds)-1])
	}
}

func TestPromptContextCanceledBeforeRun(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("never"))
	a, _ := newTestAgent(fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Prompt(ctx, "hi"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestPromptUnknownToolAndInvalidArgs(t *testing.T) {
	echo := &echoTool{}
	fake := NewFakeProvider(
		FakeToolCallEvents("call_1", "nope", `{}`),
		FakeToolCallEvents("call_2", "echo", `{"text":42}`),
		FakeTextEvents("done"),
	)
	a, _ := newTestAgent(fake, echo)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	msgs := a.Messages()
	unknown := msgs[2]
	if !unknown.IsError || !strings.Contains(textOf(unknown.Content), "unknown tool") {
		t.Fatalf("unknown-tool result = %+v", unknown)
	}
	invalid := msgs[4]
	if !invalid.IsError || !strings.Contains(textOf(invalid.Content), "invalid arguments") {
		t.Fatalf("invalid-args result = %+v", invalid)
	}
	if echo.executions() != 0 {
		t.Fatalf("executions = %d, want 0 (validation must block execution)", echo.executions())
	}
}

func TestPromptTerminateStopsRun(t *testing.T) {
	stopTool := &terminateTool{}
	fake := NewFakeProvider(
		FakeToolCallEvents("call_1", "stopnow", `{}`),
	)
	a, _ := newTestAgent(fake, stopTool)
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if len(fake.Calls()) != 1 {
		t.Fatalf("provider calls = %d, want 1 (terminate ends the run)", len(fake.Calls()))
	}
}

type terminateTool struct{}

func (*terminateTool) Name() string { return "stopnow" }

func (*terminateTool) Description() string { return "Stop the run" }

func (*terminateTool) Parameters() *Schema { return MustSchemaFor(&struct{}{}) }

func (*terminateTool) Execute(context.Context, ToolCall, func(Update)) (Result, error) {
	return Result{
		Content:   []llm.ContentBlock{llm.TextBlock{Text: "stopping"}},
		Terminate: true,
	}, nil
}

func textOf(blocks []llm.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if tb, ok := b.(llm.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}
