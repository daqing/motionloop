package agent

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daqing/motionloop/llm"
)

type delayedTool struct {
	name       string
	delay      time.Duration
	sequential bool
}

func (d *delayedTool) Name() string        { return d.name }
func (d *delayedTool) Description() string { return "Sleep then finish: " + d.name }
func (d *delayedTool) Parameters() *Schema { return MustSchemaFor(&struct{}{}) }
func (d *delayedTool) ExecutionMode() ExecutionMode {
	if d.sequential {
		return ExecutionSequential
	}
	return ExecutionParallel
}
func (d *delayedTool) Execute(ctx context.Context, call ToolCall, emit func(Update)) (Result, error) {
	select {
	case <-time.After(d.delay):
		return Result{Content: []llm.ContentBlock{llm.TextBlock{Text: d.name + " done"}}}, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func twoToolCallEvents(slowID, slowName, fastID, fastName string) []llm.StreamEvent {
	return []llm.StreamEvent{
		llm.ToolCallDelta{Index: 0, ID: slowID, Name: slowName},
		llm.ToolCallDelta{Index: 1, ID: fastID, Name: fastName},
		llm.Stop{Reason: llm.StopReasonToolUse},
	}
}

func endOrder(rec *recorder) []string {
	var order []string
	for _, ev := range rec.events {
		if te, ok := ev.(ToolExecutionEnd); ok {
			order = append(order, te.ToolName)
		}
	}
	return order
}

func TestParallelBatchCompletionVsSourceOrder(t *testing.T) {
	slow := &delayedTool{name: "slow", delay: 120 * time.Millisecond}
	fast := &delayedTool{name: "fast", delay: 10 * time.Millisecond}
	fake := NewFakeProvider(
		twoToolCallEvents("c1", "slow", "c2", "fast"),
		FakeTextEvents("done"),
	)
	rec := &recorder{}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"}, WithTools(slow, fast))
	a.Subscribe(rec.record)

	start := time.Now()
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= 120*time.Millisecond+80*time.Millisecond {
		t.Fatalf("batch took %s; calls did not run in parallel", elapsed)
	}

	if got, want := endOrder(rec), []string{"fast", "slow"}; !equalKinds(got, want) {
		t.Fatalf("tool_execution_end order = %v, want completion order %v", got, want)
	}

	msgs := a.Messages()
	tr1, tr2 := msgs[2], msgs[3]
	if tr1.ToolName != "slow" || tr2.ToolName != "fast" {
		t.Fatalf("toolResults not in assistant source order: %s then %s", tr1.ToolName, tr2.ToolName)
	}
}

func TestSequentialBatchDegrades(t *testing.T) {
	slow := &delayedTool{name: "slowseq", delay: 100 * time.Millisecond, sequential: true}
	fast := &delayedTool{name: "fastpar", delay: 10 * time.Millisecond}
	fake := NewFakeProvider(
		twoToolCallEvents("c1", "slowseq", "c2", "fastpar"),
		FakeTextEvents("done"),
	)
	rec := &recorder{}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"}, WithTools(slow, fast))
	a.Subscribe(rec.record)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got, want := endOrder(rec), []string{"slowseq", "fastpar"}; !equalKinds(got, want) {
		t.Fatalf("tool_execution_end order = %v, want source order %v (batch must degrade to sequential)", got, want)
	}
}

func TestSteeringInjectedAtTurnBoundary(t *testing.T) {
	echo := &echoTool{}
	fake := NewFakeProvider(
		FakeToolCallEvents("c1", "echo", `{"text":"x"}`),
		FakeTextEvents("done"),
	)
	rec := &recorder{}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"}, WithTools(echo))
	a.Subscribe(rec.record)

	a.Steer("new direction")
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	msgs := a.Messages()
	// user, assistant(toolcall), toolResult, steering user, assistant
	if len(msgs) != 5 {
		t.Fatalf("messages = %d, want 5: %+v", len(msgs), msgs)
	}
	steered := msgs[3]
	if steered.Role != llm.RoleUser || textOf(steered.Content) != "new direction" {
		t.Fatalf("steering message = %+v", steered)
	}

	second := fake.Calls()[1].Messages
	if second[len(second)-1].Role != llm.RoleUser || textOf(second[len(second)-1].Content) != "new direction" {
		t.Fatalf("second request missing steering message at tail: %+v", second)
	}

	// injected exactly once
	count := 0
	for _, m := range a.Messages() {
		if m.Role == llm.RoleUser && textOf(m.Content) == "new direction" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("steering message injected %d times, want 1", count)
	}
}

func TestFollowUpAndContinue(t *testing.T) {
	fake := NewFakeProvider(
		FakeTextEvents("first answer"),
		FakeTextEvents("second answer"),
	)
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"})

	if err := a.Prompt(context.Background(), "question"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	a.FollowUp("and then?")
	if err := a.Continue(context.Background()); err != nil {
		t.Fatalf("continue: %v", err)
	}

	if len(fake.Calls()) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(fake.Calls()))
	}
	second := fake.Calls()[1].Messages
	tail := second[len(second)-1]
	if tail.Role != llm.RoleUser || textOf(tail.Content) != "and then?" {
		t.Fatalf("second request tail = %+v, want follow-up message", tail)
	}
	msgs := a.Messages()
	if len(msgs) != 4 || msgs[3].Role != llm.RoleAssistant || textOf(msgs[3].Content) != "second answer" {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestContinueRequiresInputAfterAssistantTail(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("answer"))
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"})
	if err := a.Prompt(context.Background(), "q"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	err := a.Continue(context.Background())
	if err == nil || !strings.Contains(err.Error(), "requires queued steering or follow-up") {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentRunsRejected(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("ok"))
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"})
	a.Subscribe(func(ev Event) {
		if _, ok := ev.(AgentStart); ok {
			// second run attempted while the first is streaming events
			if err := a.Prompt(context.Background(), "second"); err != ErrRunInProgress {
				t.Errorf("second prompt err = %v, want ErrRunInProgress", err)
			}
		}
	})
	if err := a.Prompt(context.Background(), "first"); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
}

func TestParallelCancelNoLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	ctxTool := &delayedTool{name: "waiter", delay: time.Hour} // only exits via ctx
	fake := NewFakeProvider(
		twoToolCallEvents("c1", "waiter", "c2", "waiter"),
	)
	rec := &recorder{}
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"}, WithTools(ctxTool))
	a.Subscribe(rec.record)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = a.Prompt(ctx, "go")
	}()
	time.Sleep(50 * time.Millisecond) // let the batch start
	cancel()
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines leaked after cancel: before=%d after=%d", before, runtime.NumGoroutine())
}
