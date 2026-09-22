package coding_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/profile/coding"
	"github.com/daqing/motionloop/prompt"
	"github.com/daqing/motionloop/tools"
)

func TestCodingProfileShape(t *testing.T) {
	if coding.Profile.Name != "coding" {
		t.Fatalf("name = %q", coding.Profile.Name)
	}
	env := prompt.Environment{Cwd: "/proj", Platform: "darwin (arm64)", Date: "2026-09-22"}
	sections := coding.Profile.Prompt(env)

	for _, name := range []string{prompt.SectionIdentity, prompt.SectionEnvironment, prompt.SectionTools, prompt.SectionRules} {
		if c, ok := sections.Get(name); !ok || strings.TrimSpace(c) == "" {
			t.Fatalf("section %q missing or empty", name)
		}
	}
	rendered := sections.Render()
	for _, want := range []string{"expert coding agent", "Working directory: /proj", "Date: 2026-09-22", "old_string", "Never commit"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered prompt missing %q", want)
		}
	}

	ws, err := tools.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range coding.Profile.Tools(ws) {
		names = append(names, tool.Name())
	}
	if strings.Join(names, ",") != "bash,read,write,edit,grep,glob,ls" {
		t.Fatalf("tools = %v", names)
	}
	if coding.Profile.ThinkingLevel != llm.ThinkingMedium {
		t.Fatalf("thinking = %q", coding.Profile.ThinkingLevel)
	}
}

// TestCodingProfileEndToEnd is the M2 acceptance path with the remote
// endpoint replaced by a scripted provider: the profile-built agent reads
// code, edits it, verifies via bash, and reports — against real tools in a
// real workspace.
func TestCodingProfileEndToEnd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "greeter.go"), []byte("package main\n\nconst greeting = \"hello\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	readCall := `{"path":"greeter.go"}`
	editCall := `{"path":"greeter.go","old_string":"\"hello\"","new_string":"\"hi motionloop\""}`
	bashCall := `{"command":"cat greeter.go"}`
	fake := agent.NewFakeProvider(
		scriptToolCall("c1", "read", readCall),
		scriptToolCall("c2", "edit", editCall),
		scriptToolCall("c3", "bash", bashCall),
		agent.FakeTextEvents("changed greeting to hi motionloop and verified the file"),
	)

	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(fake, llm.Model{ProviderID: "fake", ModelID: "test"}, coding.Profile.AgentOptions(ws, prompt.DetectEnvironment(dir), llm.StreamOptions{})...)

	if err := a.Prompt(context.Background(), "change the greeting to hi motionloop"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "greeter.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "hi motionloop") {
		t.Fatalf("file not edited: %q", content)
	}

	msgs := a.Messages()
	if msgs[0].Role != llm.RoleSystem || msgs[0].Sections["identity"] == "" {
		t.Fatalf("leading system message must carry sections: %+v", msgs[0])
	}
	// the bash tool result must show the edited content (the agent verified)
	var verified bool
	for _, m := range msgs {
		if m.Role == llm.RoleToolResult && m.ToolName == "bash" && strings.Contains(textOf(m), "hi motionloop") {
			verified = true
		}
	}
	if !verified {
		t.Fatalf("bash verification missing from transcript: %+v", msgs)
	}
	if len(fake.Calls()) != 4 {
		t.Fatalf("provider calls = %d, want 4", len(fake.Calls()))
	}
}

func scriptToolCall(id, name, args string) []llm.StreamEvent {
	return []llm.StreamEvent{
		llm.ToolCallDelta{Index: 0, ID: id, Name: name},
		llm.ToolCallDelta{Index: 0, ArgumentsDelta: args},
		llm.Stop{Reason: llm.StopReasonToolUse},
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
