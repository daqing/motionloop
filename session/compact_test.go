package session

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

func m(role llm.Role, text string) llm.Message {
	return llm.Message{Role: role, Content: []llm.ContentBlock{llm.TextBlock{Text: text}}, Timestamp: 1}
}

func TestCompactedView(t *testing.T) {
	messages := []llm.Message{
		m(llm.RoleSystem, "sys"),
		m(llm.RoleUser, "u1"),
		m(llm.RoleAssistant, "a1"),
		m(llm.RoleUser, "u2"),
		m(llm.RoleAssistant, "a2"),
		m(llm.RoleUser, "u3"),
	}
	view := compactedView(messages, "the summary", 2)
	want := []llm.Message{
		messages[0],
		{Role: llm.RoleSystem, Content: []llm.ContentBlock{llm.TextBlock{Text: "# Compacted conversation summary\n\nthe summary"}}},
		messages[3],
		messages[4],
		messages[5],
	}
	if len(view) != len(want) {
		t.Fatalf("view len = %d, want %d: %+v", len(view), len(want), view)
	}
	for i := range want {
		if view[i].Role != want[i].Role {
			t.Fatalf("view[%d].Role = %s, want %s", i, view[i].Role, want[i].Role)
		}
	}
	if !strings.Contains(textOfMessage(view[1]), "the summary") {
		t.Fatalf("summary missing: %+v", view[1])
	}
	// mid-transcript system patches survive
	patch := llm.Message{Role: llm.RoleSystem, Sections: map[string]string{"rules": "v2"}}
	messages = append(messages[:2], append([]llm.Message{patch}, messages[2:]...)...)
	view = compactedView(messages, "s", 1)
	found := false
	for _, msg := range view {
		if msg.Sections != nil && msg.Sections["rules"] == "v2" {
			found = true
		}
	}
	if !found {
		t.Fatal("system patch dropped by compaction view")
	}
}

func TestTokenEstimator(t *testing.T) {
	e := DefaultEstimator()
	ascii := m(llm.RoleUser, strings.Repeat("a", 400))
	if got := e.Estimate([]llm.Message{ascii}); got != 100 {
		t.Fatalf("ascii estimate = %d, want 100", got)
	}
	cjk := m(llm.RoleUser, strings.Repeat("中", 300))
	if got := e.Estimate([]llm.Message{cjk}); got != 200 {
		t.Fatalf("cjk estimate = %d, want 200", got)
	}
}

func TestCompactionReplayConsistency(t *testing.T) {
	mgr := NewManager(t.TempDir())
	sess, err := mgr.Create("/w")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []llm.Message{
		m(llm.RoleSystem, "sys"),
		m(llm.RoleUser, "old question"),
		m(llm.RoleAssistant, "old answer"),
		m(llm.RoleUser, "recent question"),
		m(llm.RoleAssistant, "recent answer"),
	} {
		if err := sess.AppendMessage(msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := sess.AppendCompaction("old exchange summarized", 500, 2); err != nil {
		t.Fatal(err)
	}
	sess.Close()

	_, state, err := mgr.Load(sess.Path())
	if err != nil {
		t.Fatal(err)
	}
	view := state.Messages
	if len(view) != 4 { // sys, summary, recent question, recent answer
		t.Fatalf("view = %d messages: %+v", len(view), view)
	}
	if strings.Contains(textOfMessage(view[1]), "old exchange summarized") == false {
		t.Fatalf("summary missing: %+v", view[1])
	}
	for _, msg := range view {
		if strings.Contains(textOfMessage(msg), "old question") || strings.Contains(textOfMessage(msg), "old answer") {
			t.Fatalf("summarized rows leaked into replay: %+v", msg)
		}
	}
	if !strings.Contains(textOfMessage(view[2]), "recent question") {
		t.Fatalf("kept tail damaged: %+v", view[2])
	}
}

// TestCompactorTransformOncePerRun drives the full pipeline: a run whose
// transcript exceeds the threshold triggers exactly one summarization, the
// provider sees the compacted view, and the original rows survive in the
// file.
func TestCompactorTransformOncePerRun(t *testing.T) {
	mgr := NewManager(t.TempDir())
	sess, err := mgr.Create("/w")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	seed := []llm.Message{m(llm.RoleSystem, "sys")}
	for i := 0; i < 8; i++ {
		seed = append(seed, m(llm.RoleUser, strings.Repeat("body ", 100)+string(rune('a'+i))))
		seed = append(seed, m(llm.RoleAssistant, strings.Repeat("reply ", 100)))
	}
	// seed rows land in the file first (the resume flow), then seed the agent
	for _, msg := range seed {
		if err := sess.AppendMessage(msg); err != nil {
			t.Fatal(err)
		}
	}

	fake := agent.NewFakeProvider(
		agent.FakeTextEvents("SUMMARY: goals, decisions, open items"), // summarizer call
		agent.FakeTextEvents("final answer"),                          // turn 1
	)
	compactor := &Compactor{
		Sess:       sess,
		Provider:   fake,
		Model:      llm.Model{ProviderID: "fake", ModelID: "t"},
		Threshold:  600,
		KeepRecent: 4,
	}

	a := agent.New(fake, llm.Model{ProviderID: "fake", ModelID: "t"},
		agent.WithMessages(seed...),
		agent.WithTransformContext(compactor.Transform),
	)
	rec := NewRecorder(sess)
	a.Subscribe(rec.Handle)
	if err := a.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if err := rec.Err(); err != nil {
		t.Fatal(err)
	}

	calls := fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("provider calls = %d, want 2 (summarizer + one turn)", len(calls))
	}
	// the summarizer received the instruction and the aged span
	sumInput := calls[0].Messages
	if !strings.Contains(textOfMessage(sumInput[0]), "Summarize the conversation") {
		t.Fatalf("summarizer prompt = %+v", sumInput[0])
	}
	// the agent turn saw the compacted view: summary present, aged bodies gone
	turn := calls[1].Messages
	hasSummary, hasAged, hasTail := false, false, false
	for _, msg := range turn {
		txt := textOfMessage(msg)
		if strings.Contains(txt, "SUMMARY: goals") {
			hasSummary = true
		}
		if strings.Contains(txt, "body a") {
			hasAged = true
		}
		if strings.Contains(txt, "continue") {
			hasTail = true
		}
	}
	if !hasSummary || hasAged || !hasTail {
		t.Fatalf("view wrong: summary=%v aged=%v tail=%v", hasSummary, hasAged, hasTail)
	}

	// original rows survive in the file alongside the compaction entry
	_, entries, _, err := Inspect(sess.Path())
	if err != nil {
		t.Fatal(err)
	}
	messages, compactions := 0, 0
	for _, e := range entries {
		switch e.Type {
		case TypeMessage:
			messages++
		case TypeCompaction:
			compactions++
		}
	}
	if messages != len(seed)+2 { // seed rows never removed; plus the new user and assistant messages
		t.Fatalf("message rows = %d, want %d (originals must survive)", messages, len(seed)+2)
	}
	if compactions != 1 {
		t.Fatalf("compaction entries = %d, want 1", compactions)
	}

	// replay yields the same compacted view the provider saw (plus the
	// final assistant message recorded afterwards)
	_, state, err := mgr.Load(sess.Path())
	if err != nil {
		t.Fatal(err)
	}
	replayed := state.Messages[:len(state.Messages)-1]
	if !reflect.DeepEqual(replayed, calls[1].Messages) {
		t.Fatalf("replayed view differs from the live request view:\n%+v\nvs\n%+v", replayed, calls[1].Messages)
	}
}

func TestCompactorUnderThreshold(t *testing.T) {
	mgr := NewManager(t.TempDir())
	sess, _ := mgr.Create("/w")
	defer sess.Close()
	fake := agent.NewFakeProvider(agent.FakeTextEvents("ok"))
	compactor := &Compactor{Sess: sess, Provider: fake, Model: llm.Model{ProviderID: "f", ModelID: "m"}, Threshold: 100000}
	a := agent.New(fake, llm.Model{ProviderID: "f", ModelID: "m"}, agent.WithTransformContext(compactor.Transform))
	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls()) != 1 {
		t.Fatalf("summarizer must not run under the threshold: %d calls", len(fake.Calls()))
	}
}

func TestCompactorDisabled(t *testing.T) {
	c := &Compactor{Threshold: -1}
	msgs := []llm.Message{m(llm.RoleUser, strings.Repeat("x", 100000))}
	if got := c.Transform(context.Background(), msgs); len(got) != 1 {
		t.Fatal("disabled compactor must pass through")
	}
}

func textOfMessage(m llm.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if tb, ok := b.(llm.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}
