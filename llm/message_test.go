package llm

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestMessageGolden(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{
			name: "user-text",
			msg: Message{
				Role:      RoleUser,
				Content:   []ContentBlock{TextBlock{Text: "Read PLAN.md and summarize it."}},
				Timestamp: 1733234400000,
			},
		},
		{
			name: "user-image",
			msg: Message{
				Role: RoleUser,
				Content: []ContentBlock{
					TextBlock{Text: "What is in this screenshot?"},
					ImageBlock{Data: "aGVsbG8=", MimeType: "image/png"},
				},
				Timestamp: 1733234400100,
			},
		},
		{
			name: "assistant-toolcall",
			msg: Message{
				Role: RoleAssistant,
				Content: []ContentBlock{
					ThinkingBlock{Thinking: "The user wants a summary; read the plan first.", Signature: "sig1"},
					TextBlock{Text: "Reading the plan now."},
					ToolCallBlock{
						ID:        "call_1",
						Name:      "read",
						Arguments: json.RawMessage(`{"path":"PLAN.md","limit":200}`),
					},
				},
				Provider:      "anthropic",
				Model:         "claude-sonnet-4-5",
				ResponseModel: "claude-sonnet-4-5",
				ThinkingLevel: "high",
				StopReason:    StopReasonToolUse,
				Usage: &Usage{
					Input: 1024, Output: 512, CacheRead: 2048, Reasoning: 128, TotalTokens: 3712,
					Cost: Cost{Input: 0.003, Output: 0.0025, CacheRead: 0.0003, Total: 0.0058},
				},
				Timestamp: 1733234400200,
			},
		},
		{
			name: "assistant-error",
			msg: Message{
				Role:         RoleAssistant,
				Provider:     "openai",
				Model:        "gpt-5.2",
				StopReason:   StopReasonError,
				ErrorMessage: "stream disconnected",
				Timestamp:    1733234400300,
			},
		},
		{
			name: "toolresult",
			msg: Message{
				Role:       RoleToolResult,
				ToolCallID: "call_1",
				ToolName:   "read",
				Content:    []ContentBlock{TextBlock{Text: "1\t# Motionloop"}},
				Timestamp:  1733234400400,
			},
		},
		{
			name: "toolresult-error",
			msg: Message{
				Role:       RoleToolResult,
				ToolCallID: "call_2",
				ToolName:   "bash",
				Content:    []ContentBlock{TextBlock{Text: "exit status 1"}},
				IsError:    true,
				Timestamp:  1733234400500,
			},
		},
		{
			name: "system-sections",
			msg: Message{
				Role:     RoleSystem,
				Sections: map[string]string{"cwd": "/project", "preamble": "You are a coding agent."},
				ToolsAdded: []ToolDecl{
					{
						Name:        "read",
						Description: "Read a file",
						Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
					},
				},
				ToolsRemoved: []string{"write"},
				Timestamp:    1733234400600,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.MarshalIndent(tc.msg, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			encoded = append(encoded, '\n')

			path := filepath.Join("testdata", tc.name+".json")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatalf("mkdir testdata: %v", err)
				}
				if err := os.WriteFile(path, encoded, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if !bytes.Equal(bytes.TrimRight(want, "\n"), bytes.TrimRight(encoded, "\n")) {
				t.Errorf("marshaled output differs from %s:\n--- want ---\n%s--- got ---\n%s", path, want, encoded)
			}

			var decoded Message
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(normalizeMessage(t, decoded), normalizeMessage(t, tc.msg)) {
				t.Errorf("round-trip mismatch:\nwant: %+v\ngot:  %+v", tc.msg, decoded)
			}
		})
	}
}

func TestUsageGolden(t *testing.T) {
	u := Usage{
		Input: 1234, Output: 567, CacheRead: 8901, CacheWrite: 234, Reasoning: 45,
		TotalTokens: 10981,
		Cost:        Cost{Input: 0.0037, Output: 0.0085, CacheRead: 0.0009, CacheWrite: 0.0012, Total: 0.0143},
	}
	encoded, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	encoded = append(encoded, '\n')

	path := filepath.Join("testdata", "usage.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(bytes.TrimRight(want, "\n"), bytes.TrimRight(encoded, "\n")) {
		t.Errorf("marshaled output differs from %s:\n--- want ---\n%s--- got ---\n%s", path, want, encoded)
	}
	var decoded Usage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, u) {
		t.Errorf("round-trip mismatch:\nwant: %+v\ngot:  %+v", u, decoded)
	}
}

func TestMessageStringContent(t *testing.T) {
	var m Message
	in := []byte(`{"role":"user","content":"hello","timestamp":7}`)
	if err := json.Unmarshal(in, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := Message{
		Role:      RoleUser,
		Content:   []ContentBlock{TextBlock{Text: "hello"}},
		Timestamp: 7,
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %+v, want %+v", m, want)
	}
}

func TestMessageUnknownBlockType(t *testing.T) {
	in := []byte(`{"role":"user","content":[{"type":"video","url":"x"}],"timestamp":1}`)
	var m Message
	err := json.Unmarshal(in, &m)
	if err == nil {
		t.Fatal("expected error for unknown block type, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("unknown content block")) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// normalizeMessage compacts embedded raw JSON so round-trip comparison is
// independent of formatting inside Arguments and Parameters fields.
func normalizeMessage(t *testing.T, m Message) Message {
	t.Helper()
	for i, b := range m.Content {
		if tc, ok := b.(ToolCallBlock); ok {
			tc.Arguments = compactRaw(t, tc.Arguments)
			m.Content[i] = tc
		}
	}
	for i, d := range m.ToolsAdded {
		d.Parameters = compactRaw(t, d.Parameters)
		m.ToolsAdded[i] = d
	}
	return m
}

func compactRaw(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	if len(raw) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact raw JSON: %v", err)
	}
	return json.RawMessage(buf.Bytes())
}
