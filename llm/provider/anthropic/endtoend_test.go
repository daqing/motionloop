package anthropic_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/llm/provider/anthropic"
	"github.com/daqing/motionloop/tools"
)

func sse(parts ...string) string { return strings.Join(parts, "\n\n") + "\n\n" }

// TestAnthropicLoopEndToEnd wires the real anthropic provider and the real
// read tool against a scripted Messages API server: request 1 asks for a
// tool call, request 2 answers from the tool result.
func TestAnthropicLoopEndToEnd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello from anthropic\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	step1 := sse(
		`data: {"type":"message_start","message":{"usage":{"input_tokens":12}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"note.txt\"}"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
		`data: {"type":"message_stop"}`,
	)
	step2 := sse(
		`data: {"type":"message_start","message":{"usage":{"input_tokens":40,"cache_read_input_tokens":12}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"read it: hello from anthropic"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`,
		`data: {"type":"message_stop"}`,
	)
	var request int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request++
		w.Header().Set("Content-Type", "text/event-stream")
		if request == 1 {
			io.WriteString(w, step1)
			return
		}
		io.WriteString(w, step2)
	}))
	t.Cleanup(srv.Close)

	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := anthropic.New("atest", srv.URL, nil)
	a := agent.New(p, llm.Model{ProviderID: "atest", ModelID: "atest-model", Capabilities: llm.Capabilities{Cache: true}},
		agent.WithTools(tools.Read{WS: ws}),
		agent.WithSystemPrompt("be terse"),
	)
	if err := a.Prompt(context.Background(), "read the note"); err != nil {
		t.Fatal(err)
	}

	msgs := a.Messages()
	if len(msgs) != 5 {
		t.Fatalf("messages = %d, want 5", len(msgs))
	}
	tr := msgs[3]
	if tr.Role != llm.RoleToolResult || tr.ToolName != "read" || tr.IsError {
		t.Fatalf("toolResult = %+v", tr)
	}
	var final llm.TextBlock
	for _, b := range msgs[4].Content {
		if tb, ok := b.(llm.TextBlock); ok {
			final = tb
		}
	}
	if final.Text != "read it: hello from anthropic" {
		t.Fatalf("final text = %q", final.Text)
	}
	if msgs[4].Usage == nil || msgs[4].Usage.Input != 40 || msgs[4].Usage.CacheRead != 12 {
		t.Fatalf("usage = %+v", msgs[4].Usage)
	}
}
