package agent_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/session"
)

// countingTool is a trivial tool used for loadout-change assertions.
type countingTool struct{ name string }

func (c countingTool) Name() string              { return c.name }
func (c countingTool) Description() string       { return "counting tool " + c.name }
func (c countingTool) Parameters() *agent.Schema { return agent.MustSchemaFor(&struct{}{}) }
func (c countingTool) Execute(context.Context, agent.ToolCall, func(agent.Update)) (agent.Result, error) {
	return agent.Result{Content: []llm.ContentBlock{llm.TextBlock{Text: c.name}}}, nil
}

func TestWithSystemMessageAndPatchReplay(t *testing.T) {
	base := llm.Message{
		Role:      llm.RoleSystem,
		Content:   []llm.ContentBlock{llm.TextBlock{Text: "base prompt"}},
		Sections:  map[string]string{"identity": "v1", "rules": "old", "environment": "facts"},
		Timestamp: 1,
	}

	fake := agent.NewFakeProvider(
		agent.FakeTextEvents("first"),
		agent.FakeTextEvents("second"),
	)
	mgr := session.NewManager(t.TempDir())
	sess, err := mgr.Create("/w")
	if err != nil {
		t.Fatal(err)
	}
	rec := session.NewRecorder(sess)

	a := agent.New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		agent.WithSystemMessage(base),
	)
	a.Subscribe(rec.Handle)

	if err := a.Prompt(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	// mid-session patch: rules replaced, environment removed
	if err := a.PatchPrompt(map[string]string{"rules": "new rules", "environment": ""}); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}

	// the patch rides the transcript: the second request carries it
	second := fake.Calls()[1].Messages
	hasPatch := false
	for _, m := range second {
		if m.Role == llm.RoleSystem && m.Sections["rules"] == "new rules" {
			hasPatch = true
		}
	}
	if !hasPatch {
		t.Fatalf("patch message missing from second request: %+v", second)
	}

	// replay folds the prompt state correctly
	sess.Close()
	_, state, err := mgr.Load(sess.Path())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"identity": "v1", "rules": "new rules"}
	if !reflect.DeepEqual(state.Sections, want) {
		t.Fatalf("replayed sections = %v, want %v", state.Sections, want)
	}
}

func TestSetToolsRecordsLoadoutDiff(t *testing.T) {
	fake := agent.NewFakeProvider(
		agent.FakeTextEvents("first"),
		agent.FakeTextEvents("second"),
	)
	a := agent.New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		agent.WithTools(countingTool{name: "alpha"}),
	)
	if err := a.Prompt(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}

	if err := a.SetTools(countingTool{name: "alpha"}, countingTool{name: "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}

	msgs := a.Messages()
	var diff *llm.Message
	for i := range msgs {
		if msgs[i].Role == llm.RoleSystem && (len(msgs[i].ToolsAdded) > 0 || len(msgs[i].ToolsRemoved) > 0) {
			m := msgs[i]
			diff = &m
		}
	}
	if diff == nil {
		t.Fatal("loadout diff message missing from transcript")
	}
	if len(diff.ToolsAdded) != 1 || diff.ToolsAdded[0].Name != "beta" {
		t.Fatalf("toolsAdded = %+v", diff.ToolsAdded)
	}
	if len(diff.ToolsRemoved) != 0 {
		t.Fatalf("toolsRemoved = %+v", diff.ToolsRemoved)
	}

	// the new loadout is declared to the provider and replays from the session
	declared := fake.Calls()[1].Options.Tools
	if len(declared) != 2 {
		t.Fatalf("declared tools = %+v", declared)
	}
	names := []string{declared[0].Name, declared[1].Name}
	if !reflect.DeepEqual(names, []string{"alpha", "beta"}) {
		t.Fatalf("declared names = %v", names)
	}
}

func TestSetToolsRemoval(t *testing.T) {
	fake := agent.NewFakeProvider(agent.FakeTextEvents("ok"))
	a := agent.New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		agent.WithTools(countingTool{name: "alpha"}, countingTool{name: "beta"}),
	)
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetTools(countingTool{name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	msgs := a.Messages()
	last := msgs[len(msgs)-1]
	if last.Role != llm.RoleSystem || len(last.ToolsRemoved) != 1 || last.ToolsRemoved[0] != "beta" {
		t.Fatalf("diff message = %+v", last)
	}
}

func TestNoChangeNoDiffMessage(t *testing.T) {
	fake := agent.NewFakeProvider(agent.FakeTextEvents("ok"))
	a := agent.New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		agent.WithTools(countingTool{name: "alpha"}),
	)
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetTools(countingTool{name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range a.Messages()[1:] {
		if m.Role == llm.RoleSystem {
			t.Fatalf("unexpected system message for no-op loadout change: %+v", m)
		}
	}
}
