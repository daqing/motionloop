package openaicompat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daqing/motionloop/llm"
)

func sse(parts ...string) string {
	return strings.Join(parts, "\n\n") + "\n\n"
}

func sseServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

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
	srv := sseServer(t, http.StatusOK, sse(
		`data: {"choices":[{"delta":{"reasoning_content":"weighing options"}}]}`,
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":" there"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	))
	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}}},
		llm.StreamOptions{Credentials: llm.Credentials{APIKey: "k"}})

	want := []llm.StreamEvent{
		llm.ThinkingDelta{Delta: "weighing options"},
		llm.TextDelta{Delta: "Hello"},
		llm.TextDelta{Delta: " there"},
		llm.Stop{Reason: llm.StopReasonStop},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestStreamToolCallShards(t *testing.T) {
	srv := sseServer(t, http.StatusOK, sse(
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"pa"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"PLAN.md\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	))
	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "read the plan"}}}},
		llm.StreamOptions{})

	want := []llm.StreamEvent{
		llm.ToolCallDelta{Index: 0, ID: "call_1", Name: "read"},
		llm.ToolCallDelta{Index: 0, ArgumentsDelta: `{"pa`},
		llm.ToolCallDelta{Index: 0, ArgumentsDelta: `th":"PLAN.md"}`},
		llm.Stop{Reason: llm.StopReasonToolUse},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestStreamUsage(t *testing.T) {
	srv := sseServer(t, http.StatusOK, sse(
		`data: {"choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":2}}}`,
		`data: [DONE]`,
	))
	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}}},
		llm.StreamOptions{})

	want := []llm.StreamEvent{
		llm.TextDelta{Delta: "hi"},
		llm.UsageUpdate{Usage: llm.Usage{Input: 10, Output: 5, CacheRead: 4, Reasoning: 2, TotalTokens: 15}},
		llm.Stop{Reason: llm.StopReasonStop},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestStreamHTTPError(t *testing.T) {
	srv := sseServer(t, http.StatusUnauthorized, `{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`)
	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}}},
		llm.StreamOptions{})

	if len(got) != 1 {
		t.Fatalf("want exactly one Stop event, got %+v", got)
	}
	stop, ok := got[0].(llm.Stop)
	if !ok || stop.Reason != llm.StopReasonError {
		t.Fatalf("want Stop{error}, got %#v", got[0])
	}
	for _, want := range []string{"401", "Invalid API key", "test"} {
		if !strings.Contains(stop.ErrorMessage, want) {
			t.Fatalf("error message %q missing %q", stop.ErrorMessage, want)
		}
	}
}

func TestStreamInterrupted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n")
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)

	p := New("test", srv.URL, nil)
	got := mustStream(t, p, context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}}},
		llm.StreamOptions{})

	if len(got) != 2 {
		t.Fatalf("want TextDelta + Stop, got %+v", got)
	}
	if td, ok := got[0].(llm.TextDelta); !ok || td.Delta != "par" {
		t.Fatalf("first event = %#v, want TextDelta{par}", got[0])
	}
	stop, ok := got[1].(llm.Stop)
	if !ok || stop.Reason != llm.StopReasonError || !strings.Contains(stop.ErrorMessage, "stream interrupted") {
		t.Fatalf("last event = %#v, want Stop{error, stream interrupted...}", got[1])
	}
}

func TestStreamContextCancelDuringStream(t *testing.T) {
	var releaseOnce sync.Once
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	p := New("test", srv.URL, nil)
	ch, err := p.Stream(ctx, llm.Model{ProviderID: "test", ModelID: "m"},
		[]llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}}},
		llm.StreamOptions{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	first := <-ch
	if td, ok := first.(llm.TextDelta); !ok || td.Delta != "partial" {
		t.Fatalf("first event = %#v, want TextDelta{partial}", first)
	}
	cancel()

	var last llm.StreamEvent
	for ev := range ch {
		last = ev
	}
	stop, ok := last.(llm.Stop)
	if !ok || stop.Reason != llm.StopReasonAborted {
		t.Fatalf("last event = %#v, want Stop{aborted}", last)
	}

	releaseOnce.Do(func() { close(release) })
	srv.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines leaked: before=%d after=%d", before, runtime.NumGoroutine())
}

func TestStreamContextCancelBeforeRequest(t *testing.T) {
	p := New("test", "http://127.0.0.1:0", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Stream(ctx, llm.Model{ProviderID: "test", ModelID: "m"}, nil, llm.StreamOptions{})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("want context error, got %v", err)
	}
}

func TestRequestShape(t *testing.T) {
	type captured struct {
		auth string
		body map[string]any
	}
	got := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		got <- captured{auth: r.Header.Get("Authorization"), body: body}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse(`data: [DONE]`))
	}))
	t.Cleanup(srv.Close)

	p := New("test", "http://default-base.invalid", nil)
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: []llm.ContentBlock{llm.TextBlock{Text: "be brief"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.TextBlock{Text: "look"},
			llm.ImageBlock{Data: "AAAA", MimeType: "image/png"},
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolCallBlock{ID: "call_9", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)},
		}},
		{Role: llm.RoleToolResult, ToolCallID: "call_9", ToolName: "read", Content: []llm.ContentBlock{llm.TextBlock{Text: "file body"}}},
	}
	events := mustStream(t, p, context.Background(), msgs, llm.StreamOptions{
		Credentials: llm.Credentials{APIKey: "sk-test", BaseURL: srv.URL},
		Tools:       []llm.ToolDecl{{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if len(events) != 1 {
		t.Fatalf("want single Stop, got %+v", events)
	}

	c := <-got
	if c.auth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", c.auth)
	}
	if c.body["model"] != "test-model" || c.body["stream"] != true {
		t.Fatalf("model/stream wrong: %v", c.body)
	}
	if so, ok := c.body["stream_options"].(map[string]any); !ok || so["include_usage"] != true {
		t.Fatalf("stream_options wrong: %v", c.body["stream_options"])
	}

	msgsOut := c.body["messages"].([]any)
	if len(msgsOut) != 4 {
		t.Fatalf("messages len = %d, want 4", len(msgsOut))
	}
	sys := msgsOut[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "be brief" {
		t.Fatalf("system message wrong: %v", sys)
	}
	user := msgsOut[1].(map[string]any)
	parts := user["content"].([]any)
	if parts[0].(map[string]any)["type"] != "text" {
		t.Fatalf("part 0 wrong: %v", parts[0])
	}
	img := parts[1].(map[string]any)["image_url"].(map[string]any)
	if !strings.HasPrefix(img["url"].(string), "data:image/png;base64,AAAA") {
		t.Fatalf("image url wrong: %v", img)
	}
	assistant := msgsOut[2].(map[string]any)
	tc := assistant["tool_calls"].([]any)[0].(map[string]any)
	if tc["id"] != "call_9" || tc["function"].(map[string]any)["name"] != "read" {
		t.Fatalf("tool_calls wrong: %v", tc)
	}
	if _, hasContent := assistant["content"]; hasContent {
		t.Fatalf("assistant content should be omitted for pure tool calls: %v", assistant)
	}
	tool := msgsOut[3].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_9" || tool["content"] != "file body" {
		t.Fatalf("tool message wrong: %v", tool)
	}

	toolsOut := c.body["tools"].([]any)
	fn := toolsOut[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "read" || fn["description"] != "Read a file" {
		t.Fatalf("tools wrong: %v", fn)
	}
}

func TestCapabilitiesDefaults(t *testing.T) {
	p := New("test", "http://x", nil)
	if got := p.Capabilities(llm.Model{ModelID: "unknown"}); !got.Images || !got.ParallelToolCalls {
		t.Fatalf("default capabilities wrong: %+v", got)
	}
	declared := llm.Capabilities{Thinking: true, Cache: true}
	if got := p.Capabilities(llm.Model{ModelID: "m", Capabilities: declared}); got != declared {
		t.Fatalf("declared capabilities not honored: %+v", got)
	}
}
