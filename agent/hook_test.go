package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/daqing/motionloop/llm"
)

type streamingTool struct{}

func (*streamingTool) Name() string        { return "stream" }
func (*streamingTool) Description() string { return "Stream parts" }
func (*streamingTool) Parameters() *Schema { return MustSchemaFor(&struct{}{}) }
func (*streamingTool) Execute(ctx context.Context, call ToolCall, emit func(Update)) (Result, error) {
	emit(Update{Content: []llm.ContentBlock{llm.TextBlock{Text: "part 1"}}})
	emit(Update{Content: []llm.ContentBlock{llm.TextBlock{Text: "part 2"}}})
	return Result{Content: []llm.ContentBlock{llm.TextBlock{Text: "assembled"}}}, nil
}

func TestEventOrderWithToolUpdates(t *testing.T) {
	fake := NewFakeProvider(
		FakeToolCallEvents("call_1", "stream", `{}`),
		FakeTextEvents("finished"),
	)
	rec := &recorder{}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"}, WithTools(&streamingTool{}))
	a.Subscribe(rec.record)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	want := []string{
		"AgentStart", "TurnStart", "MessageStart", "MessageEnd",
		"MessageStart", "MessageUpdate", "MessageUpdate", "MessageUpdate", "MessageEnd",
		"ToolExecutionStart", "ToolExecutionUpdate", "ToolExecutionUpdate", "ToolExecutionEnd",
		"MessageStart", "MessageEnd",
		"TurnEnd",
		"TurnStart", "MessageStart", "MessageUpdate", "MessageEnd", "TurnEnd",
		"AgentEnd",
	}
	if got := rec.kinds(); !equalKinds(got, want) {
		t.Fatalf("event kinds:\nwant %v\ngot  %v", want, got)
	}

	var updates []ToolExecutionUpdate
	for _, ev := range rec.events {
		if u, ok := ev.(ToolExecutionUpdate); ok {
			updates = append(updates, u)
		}
	}
	if len(updates) != 2 || updates[0].ToolCallID != "call_1" {
		t.Fatalf("updates = %+v", updates)
	}
}

func TestBeforeToolCallBlocks(t *testing.T) {
	echo := &echoTool{}
	fake := NewFakeProvider(
		FakeToolCallEvents("call_1", "echo", `{"text":"secret"}`),
		FakeTextEvents("done"),
	)
	rec := &recorder{}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		WithTools(echo),
		WithBeforeToolCall(func(ctx context.Context, call ToolCall) BeforeToolCallResult {
			return BeforeToolCallResult{Block: true, Reason: "not allowed: " + call.Name}
		}),
	)
	a.Subscribe(rec.record)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if echo.executions() != 0 {
		t.Fatalf("executions = %d, want 0", echo.executions())
	}
	msgs := a.Messages()
	tr := msgs[2]
	if !tr.IsError || textOf(tr.Content) != "not allowed: echo" {
		t.Fatalf("blocked result = %+v", tr)
	}
	// run continues after a plain block: follow-up request happened
	if len(fake.Calls()) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(fake.Calls()))
	}
}

func TestBeforeToolCallBlockTerminate(t *testing.T) {
	fake := NewFakeProvider(FakeToolCallEvents("call_1", "echo", `{"text":"x"}`))
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		WithTools(&echoTool{}),
		WithBeforeToolCall(func(ctx context.Context, call ToolCall) BeforeToolCallResult {
			return BeforeToolCallResult{Block: true, Reason: "denied", Terminate: true}
		}),
	)
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if len(fake.Calls()) != 1 {
		t.Fatalf("provider calls = %d, want 1 (blocked+terminate ends run)", len(fake.Calls()))
	}
}

func TestAfterToolCallOverrides(t *testing.T) {
	fake := NewFakeProvider(
		FakeToolCallEvents("call_1", "echo", `{"text":"raw"}`),
		FakeTextEvents("ok"),
	)
	rec := &recorder{}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		WithTools(&echoTool{}),
		WithAfterToolCall(func(ctx context.Context, call ToolCall, result Result, isError bool) AfterToolCallResult {
			flag := true
			return AfterToolCallResult{
				Content: []llm.ContentBlock{llm.TextBlock{Text: "sanitized"}},
				Details: map[string]any{"audited": true},
				IsError: &flag,
			}
		}),
	)
	a.Subscribe(rec.record)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	msgs := a.Messages()
	tr := msgs[2]
	if textOf(tr.Content) != "sanitized" || !tr.IsError {
		t.Fatalf("overridden result = %+v", tr)
	}
	var end ToolExecutionEnd
	for _, ev := range rec.events {
		if te, ok := ev.(ToolExecutionEnd); ok {
			end = te
		}
	}
	if !end.IsError || textOf(end.Result.Content) != "sanitized" {
		t.Fatalf("ToolExecutionEnd = %+v", end)
	}
	if m, ok := end.Result.Details.(map[string]any); !ok || m["audited"] != true {
		t.Fatalf("details = %+v", end.Result.Details)
	}
}

func TestFinishTurnDecisions(t *testing.T) {
	cases := []struct {
		name     string
		decision TurnDecision
		steps    [][]llm.StreamEvent
		wantCall int
	}{
		{
			name:     "default continues on tool results",
			decision: DecisionDefault,
			steps: [][]llm.StreamEvent{
				FakeToolCallEvents("c1", "echo", `{"text":"x"}`),
				FakeTextEvents("done"),
			},
			wantCall: 2,
		},
		{
			name:     "end stops despite tool results",
			decision: DecisionEnd,
			steps: [][]llm.StreamEvent{
				FakeToolCallEvents("c1", "echo", `{"text":"x"}`),
			},
			wantCall: 1,
		},
		{
			name:     "continue forces one more request without tool results",
			decision: DecisionContinue,
			steps: [][]llm.StreamEvent{
				FakeTextEvents("first"),
				FakeTextEvents("second"),
			},
			wantCall: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := NewFakeProvider(tc.steps...)
			calls := 0
			a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
				WithTools(&echoTool{}),
				WithFinishTurn(func(ctx context.Context, turn Turn) TurnDecision {
					calls++
					if calls >= 2 {
						return DecisionDefault
					}
					return tc.decision
				}),
			)
			if err := a.Prompt(context.Background(), "go"); err != nil {
				t.Fatalf("prompt: %v", err)
			}
			if got := len(fake.Calls()); got != tc.wantCall {
				t.Fatalf("provider calls = %d, want %d", got, tc.wantCall)
			}
		})
	}
}

func TestSubscriberOrderAndSilenceAfterAgentEnd(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("hi"))
	var mu sync.Mutex
	var trace []string
	marker := func(name string) func(Event) {
		return func(ev Event) {
			mu.Lock()
			trace = append(trace, name)
			mu.Unlock()
		}
	}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"})
	a.Subscribe(marker("A"))
	a.Subscribe(marker("B"))

	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < len(trace); i++ {
		want := "A"
		if i%2 == 1 {
			want = "B"
		}
		if trace[i] != want {
			t.Fatalf("trace[%d] = %s, want %s (full: %v)", i, trace[i], want, trace)
		}
	}

	count := len(trace)
	time.Sleep(50 * time.Millisecond)
	if len(trace) != count {
		t.Fatalf("events emitted after agent_end: %d -> %d", count, len(trace))
	}
}

func TestTransformContextAndConvertToLLM(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("ok"))
	dropToolResults := func(ctx context.Context, msgs []llm.Message) []llm.Message {
		var out []llm.Message
		for _, m := range msgs {
			if m.Role != llm.RoleToolResult {
				out = append(out, m)
			}
		}
		return out
	}
	onlyLLM := func(msgs []llm.Message) []llm.Message {
		var out []llm.Message
		for _, m := range msgs {
			switch m.Role {
			case llm.RoleSystem, llm.RoleUser, llm.RoleAssistant, llm.RoleToolResult:
				out = append(out, m)
			}
		}
		return out
	}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		WithMessages(llm.Message{Role: llm.RoleToolResult, ToolCallID: "old", ToolName: "echo", Content: []llm.ContentBlock{llm.TextBlock{Text: "stale"}}, Timestamp: 1}),
		WithTransformContext(dropToolResults),
		WithConvertToLLM(onlyLLM),
	)
	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	sent := fake.Calls()[0].Messages
	for _, m := range sent {
		if m.Role == llm.RoleToolResult && m.ToolCallID == "old" {
			t.Fatalf("transform did not drop stale tool result: %+v", sent)
		}
	}
	transcript := a.Messages()
	if transcript[0].ToolCallID != "old" {
		t.Fatalf("transcript must keep the stale result: %+v", transcript[0])
	}
}

func TestHookContextCarriesCancellation(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("ok"))
	type ctxKey string
	var sawCancel bool
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		WithFinishTurn(func(ctx context.Context, turn Turn) TurnDecision {
			sawCancel = ctx.Value(ctxKey("marker")) == "yes"
			return DecisionDefault
		}),
	)
	ctx := context.WithValue(context.Background(), ctxKey("marker"), "yes")
	if err := a.Prompt(ctx, "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !sawCancel {
		t.Fatal("hook did not receive the prompt context")
	}
}

func TestAfterToolCallTerminateOverride(t *testing.T) {
	fake := NewFakeProvider(FakeToolCallEvents("c1", "echo", `{"text":"x"}`))
	yes := true
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		WithTools(&echoTool{}),
		WithAfterToolCall(func(ctx context.Context, call ToolCall, result Result, isError bool) AfterToolCallResult {
			return AfterToolCallResult{Terminate: &yes}
		}),
	)
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if len(fake.Calls()) != 1 {
		t.Fatalf("provider calls = %d, want 1 (after-tool terminate ends run)", len(fake.Calls()))
	}
}
