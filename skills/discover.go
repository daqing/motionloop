package skills

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Discover scans skill roots in order: directories containing SKILL.md
// (recursively) and root-level .md files with valid frontmatter become
// skills. Broken candidates are skipped with a warning instead of failing
// the scan; later roots override earlier ones on name collisions.
func Discover(roots []string) (list []Skill, warnings []string, err error) {
	byName := map[string]Skill{}
	for _, root := range roots {
		found, warns, err := scanRoot(root)
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, warns...)
		for _, s := range found {
			byName[s.Name] = s
		}
	}
	for _, s := range byName {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, warnings, nil
}

func scanRoot(root string) ([]Skill, []string, error) {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, nil
	}

	var found []Skill
	var warnings []string

	// loose root-level .md files count as standalone skills
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		file := filepath.Join(root, e.Name())
		if s, perr := Parse(file); perr == nil {
			found = append(found, s)
		} else {
			warnings = append(warnings, perr.Error())
		}
	}

	// every SKILL.md below the root counts, recursively
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		if s, perr := Parse(path); perr == nil {
			found = append(found, s)
		} else {
			warnings = append(warnings, perr.Error())
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return found, warnings, nil
}

// ProjectDirs collects .motionloop/skills and .agents/skills from dir and
// every ancestor up to the git repository root (dir alone without git).
// Callers enforce trust before passing project directories in.
func ProjectDirs(dir string) []string {
	root := dir
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output(); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			root = top
		}
	}
	var dirs []string
	for cur := dir; ; cur = filepath.Dir(cur) {
		for _, name := range []string{".motionloop", ".agents"} {
			candidate := filepath.Join(cur, name, "skills")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				dirs = append(dirs, candidate)
			}
		}
		if cur == root || cur == filepath.Dir(cur) {
			break
		}
	}
	return dirs
}

// DefaultDirs assembles the standard discovery roots: global motionloop,
// the cross-harness shared directory, then (when trusted) project
// directories — later roots override earlier ones on name collisions.
func DefaultDirs(home, cwd string, trusted bool) []string {
	dirs := []string{
		filepath.Join(home, ".motionloop", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
	if trusted {
		dirs = append(dirs, ProjectDirs(cwd)...)
	}
	return dirs
}

// Index renders the skills prompt section for the discovered set; empty
// when there are none.
func Index(list []Skill) string {
	if len(list) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# Skills\n\nOn-demand capability packages. When a task matches a description, load the full instructions with the skills_load tool.\n")
	for _, s := range list {
		fmt.Fprintf(&sb, "- %s: %s\n", s.Name, s.Description)
	}
	return strings.TrimRight(sb.String(), "\n")
}
