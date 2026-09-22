package agent_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/llm/provider/openaicompat"
	"github.com/daqing/motionloop/tools"
)

// TestLoopWithRealProviderAndTools wires the real openaicompat provider and
// the real read tool against a scripted SSE server: request 1 asks for a
// tool call, request 2 answers from the tool result. It is the M0
// acceptance path with the remote endpoint replaced by a local server.
func TestLoopWithRealProviderAndTools(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte("build an agent loop\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sse := func(parts ...string) string { return strings.Join(parts, "\n\n") + "\n\n" }
	step1 := sse(
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"PLAN.md\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	step2 := sse(
		`data: {"choices":[{"delta":{"content":"read the plan: "}}]}`,
		`data: {"choices":[{"delta":{"content":"agent loop"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}}`,
		`data: [DONE]`,
	)

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, step1)
			return
		}
		_, _ = io.WriteString(w, step2)
	}))
	t.Cleanup(srv.Close)

	p := openaicompat.New("itest", srv.URL, nil)
	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(p, llm.Model{ProviderID: "itest", ModelID: "itest-model"},
		agent.WithTools(tools.Read{WS: ws}),
		agent.WithSystemPrompt("be terse"),
	)

	if err := a.Prompt(context.Background(), "read the plan and summarize"); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	msgs := a.Messages()
	if len(msgs) != 5 {
		t.Fatalf("messages = %d, want 5 (system, user, assistant, toolResult, assistant)", len(msgs))
	}
	if msgs[0].Role != llm.RoleSystem {
		t.Fatalf("messages[0] = %+v, want system", msgs[0])
	}
	tr := msgs[3]
	if tr.Role != llm.RoleToolResult || tr.ToolName != "read" || tr.IsError {
		t.Fatalf("toolResult = %+v", tr)
	}
	var trText string
	for _, b := range tr.Content {
		if tb, ok := b.(llm.TextBlock); ok {
			trText = tb.Text
		}
	}
	if !strings.Contains(trText, "1\tbuild an agent loop") {
		t.Fatalf("toolResult content = %q", trText)
	}
	var finalText string
	for _, b := range msgs[4].Content {
		if tb, ok := b.(llm.TextBlock); ok {
			finalText = tb.Text
		}
	}
	if finalText != "read the plan: agent loop" {
		t.Fatalf("final assistant = %q", finalText)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	if !strings.Contains(bodies[0], `"function":{"name":"read"`) {
		t.Fatalf("first request missing tool declaration: %s", bodies[0])
	}
	if !strings.Contains(bodies[1], `"tool_call_id":"call_1"`) || !strings.Contains(bodies[1], `1\tbuild an agent loop`) {
		t.Fatalf("second request missing tool result: %s", bodies[1])
	}
}
