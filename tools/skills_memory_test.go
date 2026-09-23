package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daqing/motionloop/memory"
	"github.com/daqing/motionloop/prompt"
	"github.com/daqing/motionloop/skills"
)

func TestSkillsLoadTool(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: alpha\ndescription: does things\n---\n\nFull instructions here."), 0o644); err != nil {
		t.Fatal(err)
	}
	list, _, err := skills.Discover([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	load := &SkillsLoad{List: list}

	res, err := execTool(t, load, `{"name":"alpha"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainText(res), "Full instructions here.") {
		t.Fatalf("content = %q", mainText(res))
	}

	_, err = execTool(t, load, `{"name":"nope"}`)
	if err == nil || !strings.Contains(err.Error(), "unknown skill") || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("err = %v", err)
	}
}

func TestMemorySaveTool(t *testing.T) {
	store := memory.NewStore(t.TempDir())
	save := &MemorySave{Store: store, Slug: "ws"}

	if _, err := execTool(t, save, `{"name":"likes-vim","description":"Editor preference","type":"user","body":"Uses vim keybindings"}`); err != nil {
		t.Fatal(err)
	}
	facts, err := store.Facts("ws")
	if err != nil || len(facts) != 1 || facts[0].Name != "likes-vim" || facts[0].Body != "Uses vim keybindings" {
		t.Fatalf("facts = %+v, %v", facts, err)
	}

	_, err = execTool(t, save, `{"name":"x","description":"d","type":"bogus"}`)
	if err == nil || !strings.Contains(err.Error(), "invalid type") {
		t.Fatalf("err = %v", err)
	}
}

// TestPromptInjectionAssemblesSections is the M4 acceptance path: after a
// memory save and a skills scan, a fresh session's prompt sections carry
// both indexes.
func TestPromptInjectionAssemblesSections(t *testing.T) {
	store := memory.NewStore(t.TempDir())
	if err := store.Save("ws", memory.Fact{Name: "prefers-concise", Description: "Keep replies short", Type: memory.TypeUser}); err != nil {
		t.Fatal(err)
	}

	skillRoot := t.TempDir()
	writeSkillDir(t, skillRoot)
	list, _, err := skills.Discover([]string{skillRoot})
	if err != nil {
		t.Fatal(err)
	}

	sections := prompt.NewSections()
	sections.Set(prompt.SectionIdentity, "you are an agent")
	if idx := skills.Index(list); idx != "" {
		sections.Set(prompt.SectionSkills, idx)
	}
	memSection := memory.UsageSection()
	if idx := store.Index("ws"); idx != "" {
		memSection += "\n\n" + idx
	}
	sections.Set(prompt.SectionMemory, memSection)

	rendered := sections.Render()
	if !strings.Contains(rendered, "- alpha: does things") {
		t.Fatalf("skills index missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "prefers-concise — Keep replies short (user)") {
		t.Fatalf("memory index missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "background context") {
		t.Fatalf("memory framing missing:\n%s", rendered)
	}
}

func writeSkillDir(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: alpha\ndescription: does things\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
}
