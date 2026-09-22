package session_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/session"
)

type upperTool struct{}

type upperParams struct {
	Text string `json:"text" jsonschema:"required,description=Text to upper-case"`
}

func (upperTool) Name() string              { return "upper" }
func (upperTool) Description() string       { return "Upper-case text" }
func (upperTool) Parameters() *agent.Schema { return agent.MustSchemaFor(&upperParams{}) }
func (upperTool) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p upperParams
	if err := json.Unmarshal(call.Arguments, &p); err != nil {
		return agent.Result{}, err
	}
	return agent.Result{Content: []llm.ContentBlock{llm.TextBlock{Text: strings.ToUpper(p.Text)}}}, nil
}

// TestKillAndResume replays a recorded conversation into a fresh agent and
// continues it — the M1 acceptance path.
func TestKillAndResume(t *testing.T) {
	mgr := session.NewManager(t.TempDir())
	sess, err := mgr.Create("/proj")
	if err != nil {
		t.Fatal(err)
	}

	model := llm.Model{ProviderID: "fake", ModelID: "test"}
	first := agent.NewFakeProvider(
		agent.FakeToolCallEvents("call_1", "upper", `{"text":"motionloop"}`),
		agent.FakeTextEvents("all done"),
	)
	rec := session.NewRecorder(sess)
	a := agent.New(first, model,
		agent.WithTools(upperTool{}),
		agent.WithSystemPrompt("be terse"),
	)
	a.Subscribe(rec.Handle)
	if err := a.Prompt(context.Background(), "shout motionloop"); err != nil {
		t.Fatal(err)
	}
	if err := rec.Err(); err != nil {
		t.Fatalf("recorder: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}

	// "kill": everything below only reads the file
	sess2, state, err := mgr.Load(sess.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer sess2.Close()

	gotRoles := roles(state.Messages)
	wantRoles := []llm.Role{
		llm.RoleSystem, llm.RoleUser, llm.RoleAssistant,
		llm.RoleToolResult, llm.RoleAssistant,
	}
	if !equalRoles(gotRoles, wantRoles) {
		t.Fatalf("replayed roles = %v, want %v", gotRoles, wantRoles)
	}
	if text(state.Messages[3]) != "MOTIONLOOP" {
		t.Fatalf("replayed toolResult = %q", text(state.Messages[3]))
	}
	if state.TornTrailing {
		t.Fatal("clean session reported torn")
	}

	second := agent.NewFakeProvider(agent.FakeTextEvents("resumed answer"))
	rec2 := session.NewRecorder(sess2)
	a2 := agent.New(second, model,
		agent.WithTools(upperTool{}),
		agent.WithMessages(state.Messages...),
	)
	a2.Subscribe(rec2.Handle)
	if err := a2.Prompt(context.Background(), "next question"); err != nil {
		t.Fatal(err)
	}

	calls := second.Calls()
	if len(calls) != 1 {
		t.Fatalf("provider calls = %d", len(calls))
	}
	sent := calls[0].Messages
	if len(sent) != 6 {
		t.Fatalf("request messages = %d, want 6 (5 replayed + new user)", len(sent))
	}
	if sent[len(sent)-1].Role != llm.RoleUser || text(sent[len(sent)-1]) != "next question" {
		t.Fatalf("request tail = %+v", sent[len(sent)-1])
	}
	if text(sent[3]) != "MOTIONLOOP" {
		t.Fatalf("replayed tool result missing from request: %q", text(sent[3]))
	}
	if rec2.Err() != nil {
		t.Fatalf("recorder2: %v", rec2.Err())
	}

	// reload the file: both runs recorded, exactly once each
	_, final, err := mgr.Load(sess.Path())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(final.Messages); n != 7 {
		t.Fatalf("final message count = %d, want 7", n)
	}
	if text(final.Messages[6]) != "resumed answer" {
		t.Fatalf("final message = %q", text(final.Messages[6]))
	}
}

func roles(msgs []llm.Message) []llm.Role {
	out := make([]llm.Role, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role
	}
	return out
}

func equalRoles(a, b []llm.Role) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func text(m llm.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if tb, ok := b.(llm.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}
