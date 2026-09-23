package agent_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// ExampleAgent_minimal assembles a complete agent against a scripted
// provider — the fastest way to see the loop in action without a network.
func ExampleAgent_minimal() {
	fake := agent.NewFakeProvider(
		agent.FakeToolCallEvents("call_1", "echo", `{"text":"motionloop"}`),
		agent.FakeTextEvents("done"),
	)
	a := agent.New(fake, llm.Model{ProviderID: "fake", ModelID: "demo"},
		agent.WithTools(&echoToolForExample{}),
	)
	a.Subscribe(func(ev agent.Event) {
		if end, ok := ev.(agent.ToolExecutionEnd); ok {
			fmt.Printf("tool %s finished (error=%v)\n", end.ToolName, end.IsError)
		}
	})
	if err := a.Prompt(context.Background(), "say motionloop"); err != nil {
		fmt.Println("error:", err)
	}
	for _, m := range a.Messages() {
		fmt.Println(m.Role)
	}
	// Output:
	// tool echo finished (error=false)
	// user
	// assistant
	// toolResult
	// assistant
}

type echoToolForExample struct{}

func (*echoToolForExample) Name() string        { return "echo" }
func (*echoToolForExample) Description() string { return "Echo text back" }
func (*echoToolForExample) Parameters() *agent.Schema {
	return agent.MustSchemaFor(&struct {
		Text string `json:"text" jsonschema:"required,description=Text to echo"`
	}{})
}
func (*echoToolForExample) Execute(_ context.Context, call agent.ToolCall, _ func(agent.Update)) (agent.Result, error) {
	return agent.Result{Content: []llm.ContentBlock{llm.TextBlock{Text: strings.TrimSpace(string(call.Arguments))}}}, nil
}
