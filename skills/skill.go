// Package skills implements the Agent Skills standard: discovery of
// SKILL.md capability packages from global, shared, and trusted project
// directories, prompt-index rendering, and lenient frontmatter parsing in
// pi's spirit — broken skills warn instead of failing discovery.
package skills

import (
	"fmt"
	"os"
	"strings"
)

// Skill is one discovered capability package.
type Skill struct {
	Name        string
	Description string
	// Path is the SKILL.md location; Dir is its containing directory.
	Path string
	Dir  string
}

// Parse reads one SKILL.md and extracts its frontmatter. Frontmatter must
// carry a non-empty name and description; unknown keys are ignored.
func Parse(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("skills: read %s: %w", path, err)
	}
	fields, err := parseFrontmatter(string(data))
	if err != nil {
		return Skill{Path: path}, fmt.Errorf("skills: %s: %w", path, err)
	}
	name := strings.TrimSpace(fields["name"])
	description := strings.TrimSpace(fields["description"])
	if name == "" || description == "" {
		return Skill{Path: path}, fmt.Errorf("skills: %s: frontmatter needs non-empty name and description", path)
	}
	return Skill{
		Name:        name,
		Description: description,
		Path:        path,
		Dir:         dirOf(path),
	}, nil
}

// parseFrontmatter extracts a flat `key: value` mapping delimited by ---
// lines. It is a deliberate YAML subset: no nesting, no anchors.
func parseFrontmatter(content string) (map[string]string, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("no frontmatter")
	}
	fields := map[string]string{}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			if len(fields) == 0 {
				return nil, fmt.Errorf("empty frontmatter")
			}
			return fields, nil
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("frontmatter line %d is not key: value", i+1)
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		fields[strings.TrimSpace(key)] = value
	}
	return nil, fmt.Errorf("frontmatter not closed")
}

func dirOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i > 0 {
		return path[:i]
	}
	return "."
}
