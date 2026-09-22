package prompt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daqing/motionloop/llm"
)

func TestSectionsOrderAndRender(t *testing.T) {
	s := NewSections()
	s.Set(SectionRules, "be careful")
	s.Set(SectionIdentity, "you are motionloop")
	s.Set(SectionTools, "")
	s.Set("extra", "custom section")

	if got, want := strings.Join(s.Names(), ","), "identity,tools,rules,extra"; got != want {
		t.Fatalf("names = %q, want %q", got, want)
	}
	rendered := s.Render()
	if !strings.HasPrefix(rendered, "you are motionloop") {
		t.Fatalf("identity not first: %q", rendered)
	}
	if !strings.Contains(rendered, "be careful") || !strings.Contains(rendered, "custom section") {
		t.Fatalf("sections missing: %q", rendered)
	}
	if strings.Contains(rendered, "tools") && strings.Count(rendered, "\n\n") != 2 {
		t.Fatalf("empty section leaked into render: %q", rendered)
	}
}

func TestSectionsPatch(t *testing.T) {
	s := NewSections()
	s.Set(SectionIdentity, "v1")
	s.Set(SectionEnvironment, "facts")
	s.Set(SectionRules, "old rules")

	s.Patch(llm.Message{Sections: map[string]string{
		SectionRules:       "new rules",
		SectionEnvironment: "", // removal
	}})

	if got, _ := s.Get(SectionRules); got != "new rules" {
		t.Fatalf("rules = %q", got)
	}
	if _, ok := s.Get(SectionEnvironment); ok {
		t.Fatal("environment not removed")
	}
	if got, _ := s.Get(SectionIdentity); got != "v1" {
		t.Fatalf("identity = %q", got)
	}
}

func TestSectionsToMessage(t *testing.T) {
	s := NewSections()
	s.Set(SectionIdentity, "who you are")
	msg := s.ToMessage()
	if msg.Role != llm.RoleSystem || len(msg.Content) != 1 {
		t.Fatalf("msg = %+v", msg)
	}
	if tb, ok := msg.Content[0].(llm.TextBlock); !ok || tb.Text != "who you are" {
		t.Fatalf("content = %#v", msg.Content[0])
	}
	if msg.Sections["identity"] != "who you are" {
		t.Fatalf("sections = %v", msg.Sections)
	}
}

func TestApplyOverrideDir(t *testing.T) {
	s := NewSections()
	s.Set(SectionIdentity, "builtin")
	s.Set(SectionRules, "builtin rules")

	global := t.TempDir()
	write(t, global, "rules.md", "global rules")
	project := t.TempDir()
	write(t, project, "rules.md", "project rules")
	write(t, project, "custom.md", "custom section")

	if err := s.ApplyOverrideDir(global); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(SectionRules); got != "global rules" {
		t.Fatalf("rules = %q, want global", got)
	}
	if err := s.ApplyOverrideDir(project); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(SectionRules); got != "project rules" {
		t.Fatalf("rules = %q, want project override", got)
	}
	if got, _ := s.Get("custom"); got != "custom section" {
		t.Fatalf("custom = %q", got)
	}
	if got, _ := s.Get(SectionIdentity); got != "builtin" {
		t.Fatalf("identity = %q, want builtin untouched", got)
	}

	if err := s.ApplyOverrideDir(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing dir must be a no-op: %v", err)
	}
}

func TestDetectEnvironmentGit(t *testing.T) {
	env := DetectEnvironment(t.TempDir())
	if env.Cwd == "" || env.Platform == "" || env.Date == "" {
		t.Fatalf("env = %+v", env)
	}
	if env.Git != nil {
		t.Fatalf("plain temp dir reported git %+v", env.Git)
	}
	if !strings.Contains(EnvironmentSection(env), "not a repository") {
		t.Fatalf("section = %q", EnvironmentSection(env))
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@test")
	run("config", "user.name", "test")
	write(t, repo, "f.txt", "x")
	run("add", "f.txt")
	run("commit", "-qm", "init")

	clean := DetectEnvironment(repo)
	if clean.Git == nil || clean.Git.Branch == "" || clean.Git.Dirty {
		t.Fatalf("clean git = %+v", clean.Git)
	}
	if !strings.Contains(EnvironmentSection(clean), "clean working tree") {
		t.Fatalf("section = %q", EnvironmentSection(clean))
	}

	write(t, repo, "f.txt", "changed")
	dirty := DetectEnvironment(repo)
	if dirty.Git == nil || !dirty.Git.Dirty {
		t.Fatalf("dirty git = %+v", dirty.Git)
	}
	if !strings.Contains(EnvironmentSection(dirty), "uncommitted changes") {
		t.Fatalf("section = %q", EnvironmentSection(dirty))
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
