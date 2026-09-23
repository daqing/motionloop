package skills

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, sub, name, description string) {
	t.Helper()
	skillDir := filepath.Join(dir, sub)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	cases := []struct {
		name    string
		content string
		ok      bool
	}{
		{"valid", "---\nname: a\ndescription: does a thing\n---\nbody", true},
		{"quoted values", "---\nname: \"a\"\ndescription: 'does a thing'\n---\n", true},
		{"extra keys ignored", "---\nname: a\ndescription: d\nversion: 2\n---\n", true},
		{"comments and blanks", "---\n# comment\n\nname: a\ndescription: d\n---\n", true},
		{"no frontmatter", "# just markdown\n", false},
		{"unclosed frontmatter", "---\nname: a\n", false},
		{"missing description", "---\nname: a\n---\n", false},
		{"missing name", "---\ndescription: d\n---\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Parse(path)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDiscoverMatrix(t *testing.T) {
	global := t.TempDir()
	project := t.TempDir()

	writeSkill(t, global, "alpha", "alpha", "global skill")
	writeSkill(t, global, "group/nested", "nested", "nested skill")
	writeSkill(t, project, "beta", "beta", "project skill")
	// duplicate name: project root listed later wins
	writeSkill(t, project, "alpha", "alpha", "project override")

	// loose root .md with frontmatter counts
	if err := os.WriteFile(filepath.Join(global, "loose.md"), []byte("---\nname: loose\ndescription: root file\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// broken candidates warn instead of failing
	if err := os.WriteFile(filepath.Join(global, "broken.md"), []byte("no frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, global, "nodesc", "nodesc", "") // missing description -> warning

	list, warnings, err := Discover([]string{global, project})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v", warnings)
	}

	byName := map[string]Skill{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if len(byName) != 4 { // alpha, beta, loose, nested — nodesc is skipped
		t.Fatalf("skills = %v", namesOf(byName))
	}
	if byName["alpha"].Description != "project override" {
		t.Fatalf("later root must override on collision: %+v", byName["alpha"])
	}
	if byName["nested"].Dir == "" || byName["loose"].Description != "root file" {
		t.Fatalf("nested/loose discovery broken: %+v", byName)
	}
}

func TestDiscoverEmptyAndMissingRoots(t *testing.T) {
	list, warnings, err := Discover([]string{
		filepath.Join(t.TempDir(), "absent"),
		t.TempDir(),
	})
	if err != nil || len(list) != 0 || len(warnings) != 0 {
		t.Fatalf("list = %v warnings = %v err = %v", list, warnings, err)
	}
}

func TestProjectDirsToGitRoot(t *testing.T) {
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
	for _, d := range []string{
		filepath.Join(repo, ".agents", "skills"),
		filepath.Join(repo, "sub", ".motionloop", "skills"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	dirs := ProjectDirs(filepath.Join(repo, "sub"))
	joined := strings.Join(dirs, "\n")
	if !strings.Contains(joined, filepath.Join(repo, ".agents", "skills")) {
		t.Fatalf("root .agents/skills not found: %v", dirs)
	}
	if !strings.Contains(joined, filepath.Join(repo, "sub", ".motionloop", "skills")) {
		t.Fatalf("nested .motionloop/skills not found: %v", dirs)
	}
	// ancestors outside the repo are not scanned (nothing above repo root)
	for _, d := range dirs {
		if !strings.HasPrefix(d, repo) {
			t.Fatalf("dir escapes repo: %s", d)
		}
	}
}

func TestIndex(t *testing.T) {
	if got := Index(nil); got != "" {
		t.Fatalf("empty index = %q", got)
	}
	got := Index([]Skill{
		{Name: "b", Description: "second"},
		{Name: "a", Description: "first"},
	})
	for _, want := range []string{"# Skills", "skills_load", "- a: first", "- b: second"} {
		if !strings.Contains(got, want) {
			t.Fatalf("index missing %q:\n%s", want, got)
		}
	}
}

// TestDiscoverRealSharedSkills verifies discovery against this machine's
// real ~/.agents/skills directory when present (the M4 acceptance path).
func TestDiscoverRealSharedSkills(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(home, ".agents", "skills")
	if info, err := os.Stat(shared); err != nil || !info.IsDir() {
		t.Skip("~/.agents/skills not present")
	}
	list, _, err := Discover([]string{shared})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("real shared skills dir yielded nothing")
	}
	loaded := 0
	for _, s := range list {
		data, err := os.ReadFile(s.Path)
		if err != nil || len(data) == 0 {
			t.Fatalf("skill %s unreadable: %v", s.Name, err)
		}
		loaded++
		if loaded >= 5 {
			break
		}
	}
	t.Logf("discovered %d real skills from %s (sampled %d readable)", len(list), shared, loaded)
}

func namesOf(m map[string]Skill) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
