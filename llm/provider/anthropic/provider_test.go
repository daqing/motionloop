package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/daqing/motionloop/llm"
)

func sse(parts ...string) string { return strings.Join(parts, "\n\n") + "\n\n" }

func collect(ch <-chan llm.StreamEvent) []llm.StreamEvent {
	var evs []llm.StreamEvent
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func mustStream(t *testing.T, p *Provider, ctx context.Context, msgs []llm.Message, opts llm.StreamOptions) []llm.StreamEvent {
	t.Helper()
	ch, err := p.Stream(ctx, llm.Model{ProviderID: "test", ModelID: "test-model"}, msgs, opts)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	return collect(ch)
}

func TestStreamText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"usage":{"input_tokens":25}}}`,
			`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}`,
			`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			`event: message_stop`+"\n"+`data: {"type":"message_stop"}`,
		))
	}))
	t.Cleanup(srv.Close)

	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}},
	}, llm.StreamOptions{Credentials: llm.Credentials{APIKey: "sk"}})

	want := []llm.StreamEvent{
		llm.TextDelta{Delta: "Hello"},
		llm.TextDelta{Delta: " there"},
		llm.UsageUpdate{Usage: llm.Usage{Input: 25, Output: 7, TotalTokens: 32}},
		llm.Stop{Reason: llm.StopReasonStop},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestStreamThinkingAndToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"pondering"}}`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read"}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"pa"}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"th\":1}"}}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
			`data: {"type":"message_stop"}`,
		))
	}))
	t.Cleanup(srv.Close)

	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "go"}}},
	}, llm.StreamOptions{})

	want := []llm.StreamEvent{
		llm.ThinkingDelta{Delta: "pondering"},
		llm.ToolCallDelta{Index: 1, ID: "toolu_1", Name: "read"},
		llm.ToolCallDelta{Index: 1, ArgumentsDelta: `{"pa`},
		llm.ToolCallDelta{Index: 1, ArgumentsDelta: `th":1}`},
		llm.UsageUpdate{Usage: llm.Usage{Input: 10, Output: 5, TotalTokens: 15}},
		llm.Stop{Reason: llm.StopReasonToolUse},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	}))
	t.Cleanup(srv.Close)

	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}},
	}, llm.StreamOptions{})

	if len(got) != 1 {
		t.Fatalf("events = %+v", got)
	}
	stop, ok := got[0].(llm.Stop)
	if !ok || stop.Reason != llm.StopReasonError {
		t.Fatalf("event = %#v", got[0])
	}
	for _, want := range []string{"401", "invalid x-api-key", "test"} {
		if !strings.Contains(stop.ErrorMessage, want) {
			t.Fatalf("message %q missing %q", stop.ErrorMessage, want)
		}
	}
}

func TestStreamErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	t.Cleanup(srv.Close)

	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}},
	}, llm.StreamOptions{})
	stop, ok := got[0].(llm.Stop)
	if !ok || stop.Reason != llm.StopReasonError || !strings.Contains(stop.ErrorMessage, "Overloaded") {
		t.Fatalf("events = %+v", got)
	}
}

func TestStreamInterrupted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"par\"}}\n\n")
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)

	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}},
	}, llm.StreamOptions{})
	stop, ok := got[len(got)-1].(llm.Stop)
	if !ok || stop.Reason != llm.StopReasonError || !strings.Contains(stop.ErrorMessage, "stream interrupted") {
		t.Fatalf("events = %+v", got)
	}
}

func TestRequestShape(t *testing.T) {
	type captured struct {
		path string
		key  string
		ver  string
		body map[string]any
	}
	got := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		got <- captured{path: r.URL.Path, key: r.Header.Get("x-api-key"), ver: r.Header.Get("anthropic-version"), body: body}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"message_stop"}`))
	}))
	t.Cleanup(srv.Close)

	p := New("test", srv.URL, nil)
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: []llm.ContentBlock{llm.TextBlock{Text: "be brief"}}},
		{Role: llm.RoleSystem, Sections: map[string]string{"rules": "v2"}}, // patch message: no request text
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.TextBlock{Text: "look"},
			llm.ImageBlock{Data: "AAAA", MimeType: "image/png"},
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolCallBlock{ID: "toolu_9", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)},
		}},
		{Role: llm.RoleToolResult, ToolCallID: "toolu_9", ToolName: "read", Content: []llm.ContentBlock{llm.TextBlock{Text: "body"}}, IsError: true},
	}
	cacheModel := llm.Model{ProviderID: "test", ModelID: "test-model", Capabilities: llm.Capabilities{Cache: true}}
	ch, err := p.Stream(context.Background(), cacheModel, msgs, llm.StreamOptions{
		Credentials: llm.Credentials{APIKey: "sk-ant"},
		Tools:       []llm.ToolDecl{{Name: "read", Description: "Read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	events := collect(ch)
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}

	c := <-got
	if c.path != "/v1/messages" || c.key != "sk-ant" || c.ver != "2023-06-01" {
		t.Fatalf("request meta = %+v", c)
	}
	if c.body["model"] != "test-model" || c.body["stream"] != true || c.body["max_tokens"] == nil {
		t.Fatalf("body basics wrong: %v", c.body)
	}

	system, ok := c.body["system"].([]any)
	if !ok || len(system) != 1 {
		t.Fatalf("patch message must not add request-side system text: %v", c.body["system"])
	}
	if system[0].(map[string]any)["text"] != "be brief" {
		t.Fatalf("system text = %v", system[0])
	}
	if system[0].(map[string]any)["cache_control"] == nil {
		t.Fatalf("cache_control missing from last system block (cache-capable model)")
	}

	amsgs := c.body["messages"].([]any)
	if len(amsgs) != 3 {
		t.Fatalf("messages = %d, want 3 (user, assistant, merged tool-result user)", len(amsgs))
	}
	user := amsgs[0].(map[string]any)
	blocks := user["content"].([]any)
	if blocks[0].(map[string]any)["type"] != "text" || blocks[1].(map[string]any)["type"] != "image" {
		t.Fatalf("user blocks = %v", blocks)
	}
	if src := blocks[1].(map[string]any)["source"].(map[string]any); src["media_type"] != "image/png" || src["data"] != "AAAA" {
		t.Fatalf("image source = %v", src)
	}
	assistant := amsgs[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Fatalf("assistant role = %v", assistant)
	}
	toolUse := assistant["content"].([]any)[0].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["id"] != "toolu_9" || toolUse["input"].(map[string]any)["path"] != "x" {
		t.Fatalf("tool_use block = %v", toolUse)
	}
	toolResultMsg := amsgs[2].(map[string]any)
	if toolResultMsg["role"] != "user" {
		t.Fatalf("tool result must be a user message: %v", toolResultMsg)
	}
	tr := toolResultMsg["content"].([]any)[0].(map[string]any)
	if tr["type"] != "tool_result" || tr["tool_use_id"] != "toolu_9" || tr["text"] != "body" || tr["is_error"] != true {
		t.Fatalf("tool_result block = %v", tr)
	}

	tools := c.body["tools"].([]any)
	fn := tools[0].(map[string]any)
	if fn["name"] != "read" || fn["input_schema"] == nil {
		t.Fatalf("tools = %v", fn)
	}
}

func TestThinkingBudgetAndMaxTokens(t *testing.T) {
	got := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		got <- body
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"message_stop"}`))
	}))
	t.Cleanup(srv.Close)

	p := New("test", srv.URL, nil)
	model := llm.Model{ProviderID: "test", ModelID: "m", Capabilities: llm.Capabilities{Thinking: true}}
	msgs := []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}}}

	ch, err := p.Stream(context.Background(), model, msgs, llm.StreamOptions{ThinkingLevel: llm.ThinkingMedium})
	if err != nil {
		t.Fatal(err)
	}
	collect(ch)
	body := <-got
	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" || thinking["budget_tokens"] != float64(8192) {
		t.Fatalf("thinking = %v", body["thinking"])
	}
	if body["max_tokens"].(float64) <= 8192 {
		t.Fatalf("max_tokens must exceed the thinking budget: %v", body["max_tokens"])
	}

	// non-thinking model: no thinking block even when the level is set
	ch, err = p.Stream(context.Background(), llm.Model{ProviderID: "test", ModelID: "m"}, msgs, llm.StreamOptions{ThinkingLevel: llm.ThinkingHigh})
	if err != nil {
		t.Fatal(err)
	}
	collect(ch)
	body = <-got
	if _, has := body["thinking"]; has {
		t.Fatalf("thinking block must be omitted for non-thinking models: %v", body)
	}
	if body["max_tokens"] != float64(defaultMaxTokens) {
		t.Fatalf("default max_tokens = %v", body["max_tokens"])
	}
}
