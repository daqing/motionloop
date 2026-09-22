package session

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/daqing/motionloop/llm"
)

var update = flag.Bool("update", false, "rewrite golden files")

func msg(role llm.Role, text string) llm.Message {
	return llm.Message{Role: role, Content: []llm.ContentBlock{llm.TextBlock{Text: text}}, Timestamp: 1}
}

func TestGoldenFormat(t *testing.T) {
	h := Header{
		Type:      "session",
		Version:   1,
		ID:        "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0",
		Timestamp: "2026-09-22T08:00:00.000Z",
		Cwd:       "/Users/dev/project",
	}
	entries := []Entry{
		{
			Type: "message", ID: "a1b2c3d4", Timestamp: "2026-09-22T08:00:01.000Z",
			Message: &llm.Message{
				Role:       llm.RoleSystem,
				Content:    []llm.ContentBlock{llm.TextBlock{Text: "You are a coding agent."}},
				Sections:   map[string]string{"cwd": "/Users/dev/project"},
				ToolsAdded: []llm.ToolDecl{{Name: "read"}, {Name: "bash"}},
				Timestamp:  1733234400000,
			},
		},
		{
			Type: "message", ID: "b2c3d4e5", ParentID: "a1b2c3d4", Timestamp: "2026-09-22T08:00:02.000Z",
			Message: &llm.Message{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "fix the test"}}, Timestamp: 1733234401000},
		},
		{
			Type: "message", ID: "c3d4e5f6", ParentID: "b2c3d4e5", Timestamp: "2026-09-22T08:00:03.000Z",
			Message: &llm.Message{
				Role:     llm.RoleAssistant,
				Content:  []llm.ContentBlock{llm.ToolCallBlock{ID: "call_1", Name: "bash", Arguments: json.RawMessage(`{"command":"go test ./..."}`)}},
				Provider: "openai", Model: "gpt-5.2", StopReason: llm.StopReasonToolUse,
				Timestamp: 1733234402000,
			},
		},
		{
			Type: "message", ID: "d4e5f6a7", ParentID: "c3d4e5f6", Timestamp: "2026-09-22T08:00:04.000Z",
			Message: &llm.Message{Role: llm.RoleToolResult, ToolCallID: "call_1", ToolName: "bash", Content: []llm.ContentBlock{llm.TextBlock{Text: "ok"}}, Timestamp: 1733234403000},
		},
		{
			Type: "model_change", ID: "e5f6a7b8", ParentID: "d4e5f6a7", Timestamp: "2026-09-22T08:05:00.000Z",
			Provider: "anthropic", ModelID: "claude-sonnet-4-5",
		},
		{
			Type: "thinking_level_change", ID: "f6a7b8c9", ParentID: "e5f6a7b8", Timestamp: "2026-09-22T08:06:00.000Z",
			ThinkingLevel: llm.ThinkingHigh,
		},
		{
			Type: "usage", ID: "a7b8c9d0", ParentID: "f6a7b8c9", Timestamp: "2026-09-22T08:07:00.000Z",
			Usage: &llm.Usage{Input: 1000, Output: 300, TotalTokens: 1300, Cost: llm.Cost{Total: 0.01}},
		},
	}

	var buf bytes.Buffer
	line, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	buf.Write(line)
	buf.WriteByte('\n')
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	path := filepath.Join("testdata", "session.jsonl")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run -update to create): %v", err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		t.Fatalf("wire format drift:\n--- want ---\n%s--- got ---\n%s", want, buf.Bytes())
	}

	// the golden bytes must also round-trip through the parser
	pf, err := parseFile(bytes.NewReader(want))
	if err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if !reflect.DeepEqual(pf.header, h) {
		t.Fatalf("header round-trip: %+v vs %+v", pf.header, h)
	}
	if len(pf.entries) != len(entries) {
		t.Fatalf("entries = %d, want %d", len(pf.entries), len(entries))
	}
	for i := range entries {
		got := pf.entries[i]
		exp := entries[i]
		if got.Type != exp.Type || got.ID != exp.ID || got.ParentID != exp.ParentID || got.Timestamp != exp.Timestamp {
			t.Fatalf("entry %d envelope drift: %+v vs %+v", i, got, exp)
		}
	}
}

func TestTornTrailingLineDropped(t *testing.T) {
	mgr := NewManager(t.TempDir())
	s, err := mgr.Create("/w")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(msg(llm.RoleUser, "one")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(msg(llm.RoleAssistant, "two")); err != nil {
		t.Fatal(err)
	}
	tip := s.TipID()
	s.Close()

	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"message","id":"torn12`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	s2, st, err := mgr.Load(s.Path())
	if err != nil {
		t.Fatalf("load with torn tail: %v", err)
	}
	defer s2.Close()
	if !st.TornTrailing {
		t.Fatal("TornTrailing not reported")
	}
	if len(st.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(st.Messages))
	}
	if st.TipID != tip {
		t.Fatalf("tip = %s, want last intact entry %s", st.TipID, tip)
	}
}

func TestCorruptMidFileRejected(t *testing.T) {
	mgr := NewManager(t.TempDir())
	s, err := mgr.Create("/w")
	if err != nil {
		t.Fatal(err)
	}
	s.AppendMessage(msg(llm.RoleUser, "one"))
	s.Close()

	f, _ := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("{\"type\": oops}\n{\"type\":\"message\",\"id\":\"ok2\"}\n")
	f.Close()

	if _, _, err := mgr.Load(s.Path()); err == nil {
		t.Fatal("want corruption error for unparseable mid-file line")
	}
}

func TestReplayFold(t *testing.T) {
	mgr := NewManager(t.TempDir())
	s, err := mgr.Create("/w")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	sys := llm.Message{
		Role:       llm.RoleSystem,
		Content:    []llm.ContentBlock{llm.TextBlock{Text: ""}},
		Sections:   map[string]string{"preamble": "v1", "cwd": "/w"},
		ToolsAdded: []llm.ToolDecl{{Name: "read"}, {Name: "bash"}, {Name: "write"}},
	}
	if err := s.AppendMessage(sys); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(msg(llm.RoleUser, "go")); err != nil {
		t.Fatal(err)
	}
	patch := llm.Message{
		Role:         llm.RoleSystem,
		Sections:     map[string]string{"preamble": "v2", "cwd": ""}, // empty value removes
		ToolsAdded:   []llm.ToolDecl{{Name: "grep"}},
		ToolsRemoved: []string{"write"},
	}
	if err := s.AppendMessage(patch); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendModelChange("anthropic", "claude-sonnet-4-5"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendThinkingLevelChange(llm.ThinkingMedium); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendUsage(llm.Usage{Input: 1, Output: 2, TotalTokens: 3}); err != nil {
		t.Fatal(err)
	}

	_, st, err := mgr.Load(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(st.Messages))
	}
	wantSections := map[string]string{"preamble": "v2"}
	if !reflect.DeepEqual(st.Sections, wantSections) {
		t.Fatalf("sections = %v, want %v", st.Sections, wantSections)
	}
	wantTools := []string{"read", "bash", "grep"}
	if !reflect.DeepEqual(st.Tools, wantTools) {
		t.Fatalf("tools = %v, want %v", st.Tools, wantTools)
	}
	if st.Model == nil || st.Model.ProviderID != "anthropic" || st.Model.ModelID != "claude-sonnet-4-5" {
		t.Fatalf("model = %+v", st.Model)
	}
	if st.ThinkingLevel != llm.ThinkingMedium {
		t.Fatalf("thinking level = %q", st.ThinkingLevel)
	}
	if st.Usage == nil || st.Usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v", st.Usage)
	}
}

func TestForkIndependentEvolution(t *testing.T) {
	mgr := NewManager(t.TempDir())
	a, err := mgr.Create("/w")
	if err != nil {
		t.Fatal(err)
	}
	a.AppendMessage(msg(llm.RoleUser, "shared q"))
	a.AppendMessage(msg(llm.RoleAssistant, "shared a"))

	b, err := a.Fork()
	if err != nil {
		t.Fatal(err)
	}

	a.AppendMessage(msg(llm.RoleUser, "only in A"))
	b.AppendMessage(msg(llm.RoleUser, "only in B"))
	b.AppendMessage(msg(llm.RoleAssistant, "B answer"))
	a.Close()
	b.Close()

	_, stA, err := mgr.Load(a.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(stA.Messages) != 3 || textOf(stA.Messages[2]) != "only in A" {
		t.Fatalf("A state = %d messages, last %q", len(stA.Messages), textOf(stA.Messages[len(stA.Messages)-1]))
	}

	_, stB, err := mgr.Load(b.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(stB.Messages) != 4 {
		t.Fatalf("B messages = %d, want 4 (parent prefix + 2 own)", len(stB.Messages))
	}
	if textOf(stB.Messages[2]) != "only in B" || textOf(stB.Messages[3]) != "B answer" {
		t.Fatalf("B chain contains A's post-fork message: %v", texts(stB.Messages))
	}
	if b.Header().ParentSession != a.Path() {
		t.Fatalf("fork header parentSession = %q", b.Header().ParentSession)
	}
}

func TestInPlaceBranch(t *testing.T) {
	mgr := NewManager(t.TempDir())
	a, err := mgr.Create("/w")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.AppendMessage(msg(llm.RoleUser, "one"))
	a.AppendMessage(msg(llm.RoleUser, "two"))
	afterTwo := a.TipID()
	a.AppendMessage(msg(llm.RoleUser, "three"))

	if err := a.Branch(afterTwo); err != nil {
		t.Fatal(err)
	}
	if err := a.AppendMessage(msg(llm.RoleUser, "branch")); err != nil {
		t.Fatal(err)
	}

	_, st, err := mgr.Load(a.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(st.Messages); !reflect.DeepEqual(got, []string{"one", "two", "branch"}) {
		t.Fatalf("branch chain = %v, want [one two branch]", got)
	}
}

func TestBranchUnknownEntry(t *testing.T) {
	mgr := NewManager(t.TempDir())
	a, _ := mgr.Create("/w")
	defer a.Close()
	if err := a.Branch("nope1234"); err == nil || !strings.Contains(err.Error(), "unknown entry") {
		t.Fatalf("err = %v", err)
	}
}

func TestWorkspaceSlug(t *testing.T) {
	if got, want := WorkspaceSlug("/Users/dev/my project"), "Users-dev-my project"; got != want {
		t.Fatalf("slug = %q, want %q", got, want)
	}
	if got := WorkspaceSlug("/"); got != "default" {
		t.Fatalf("root slug = %q", got)
	}
}

func textOf(m llm.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if tb, ok := b.(llm.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

func texts(msgs []llm.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = textOf(m)
	}
	return out
}
