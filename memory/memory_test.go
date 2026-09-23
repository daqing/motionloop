package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndIndex(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.Save("ws1", Fact{
		Name:        "prefers-concise-answers",
		Description: "User wants short, conclusion-first replies",
		Type:        TypeUser,
		Body:        "Lead with the outcome; details after.",
	}); err != nil {
		t.Fatal(err)
	}
	idx := store.Index("ws1")
	if !strings.Contains(idx, "# Memory Index") || !strings.Contains(idx, "prefers-concise-answers — User wants short, conclusion-first replies (user)") {
		t.Fatalf("index = %q", idx)
	}

	facts, err := store.Facts("ws1")
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts = %v, %v", facts, err)
	}
	if facts[0].Body != "Lead with the outcome; details after." {
		t.Fatalf("body = %q", facts[0].Body)
	}

	// same name updates in place, index stays consistent
	if err := store.Save("ws1", Fact{Name: "prefers-concise-answers", Description: "Updated summary", Type: TypeFeedback}); err != nil {
		t.Fatal(err)
	}
	if idx := store.Index("ws1"); strings.Count(idx, "prefers-concise-answers") != 1 {
		t.Fatalf("index has duplicates:\n%s", idx)
	}
	if facts, _ := store.Facts("ws1"); len(facts) != 1 || facts[0].Description != "Updated summary" {
		t.Fatalf("update did not replace: %+v", facts)
	}
}

func TestWorkspaceIsolation(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save("ws-a", Fact{Name: "fact-a", Description: "only in A", Type: TypeProject}); err != nil {
		t.Fatal(err)
	}
	if idx := store.Index("ws-b"); idx != "" {
		t.Fatalf("ws-b sees ws-a facts: %q", idx)
	}
	// facts land in per-slug directories
	if _, err := os.Stat(filepath.Join(store.Root, "ws-a", "fact-a.md")); err != nil {
		t.Fatal(err)
	}
}

func TestValidation(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save("ws", Fact{Name: "", Description: "d", Type: TypeUser}); err == nil {
		t.Fatal("empty name accepted")
	}
	if err := store.Save("ws", Fact{Name: "n", Description: "", Type: TypeUser}); err == nil {
		t.Fatal("empty description accepted")
	}
	if err := store.Save("ws", Fact{Name: "n", Description: "d", Type: "random"}); err == nil || !strings.Contains(err.Error(), "invalid type") {
		t.Fatalf("bad type err = %v", err)
	}
}

func TestNameSanitized(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save("ws", Fact{Name: "User Prefers 中文!", Description: "d", Type: TypeUser}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "ws", "user-prefers.md")); err != nil {
		t.Fatalf("sanitized file missing: %v", err)
	}
}

func TestIndexEmptyWhenAbsent(t *testing.T) {
	if got := NewStore(t.TempDir()).Index("nothing"); got != "" {
		t.Fatalf("index = %q", got)
	}
}

func TestUsageSection(t *testing.T) {
	s := UsageSection()
	if !strings.Contains(s, "background context") || !strings.Contains(s, "memory_save") {
		t.Fatalf("usage section = %q", s)
	}
}
