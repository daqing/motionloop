// Package prompt assembles system prompts from named sections and injects
// environment facts. Section patches ride on system messages and fold back
// on session replay, so the prompt is transcript state — there is no
// separate prompt store.
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daqing/motionloop/llm"
)

// Canonical section names, in assembly order; extra sections append after
// them in insertion order.
const (
	SectionIdentity    = "identity"
	SectionEnvironment = "environment"
	SectionTools       = "tools"
	SectionSkills      = "skills"
	SectionMemory      = "memory"
	SectionRules       = "rules"
)

var canonical = []string{
	SectionIdentity,
	SectionEnvironment,
	SectionTools,
	SectionSkills,
	SectionMemory,
	SectionRules,
}

func isCanonical(name string) bool {
	for _, n := range canonical {
		if n == name {
			return true
		}
	}
	return false
}

// Sections is an ordered set of named prompt sections; empty sections are
// skipped when rendering.
type Sections struct {
	extra []string
	vals  map[string]string
}

// NewSections returns an empty section set.
func NewSections() *Sections { return &Sections{vals: map[string]string{}} }

// Set adds or replaces one section.
func (s *Sections) Set(name, content string) *Sections {
	if _, ok := s.vals[name]; !ok && !isCanonical(name) {
		s.extra = append(s.extra, name)
	}
	s.vals[name] = content
	return s
}

// Remove deletes one section.
func (s *Sections) Remove(name string) { delete(s.vals, name) }

// Get returns one section's content.
func (s *Sections) Get(name string) (string, bool) {
	c, ok := s.vals[name]
	return c, ok
}

// Names lists present sections: canonical order first, then extras.
func (s *Sections) Names() []string {
	var names []string
	for _, n := range canonical {
		if _, ok := s.vals[n]; ok {
			names = append(names, n)
		}
	}
	for _, n := range s.extra {
		if _, ok := s.vals[n]; ok {
			names = append(names, n)
		}
	}
	return names
}

// Render joins non-empty sections with blank lines.
func (s *Sections) Render() string {
	var parts []string
	for _, n := range s.Names() {
		if c := s.vals[n]; strings.TrimSpace(c) != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, "\n\n")
}

// Patch applies one system message's section patches: named replacement,
// empty value removal — the same semantics session replay folds with.
func (s *Sections) Patch(msg llm.Message) {
	for k, v := range msg.Sections {
		if v == "" {
			s.Remove(k)
		} else {
			s.Set(k, v)
		}
	}
}

// ToMessage renders the sections into the leading system message: content
// for providers, sections for replay.
func (s *Sections) ToMessage() llm.Message {
	return llm.Message{
		Role:      llm.RoleSystem,
		Content:   []llm.ContentBlock{llm.TextBlock{Text: s.Render()}},
		Sections:  s.snapshot(),
		Timestamp: time.Now().UnixMilli(),
	}
}

func (s *Sections) snapshot() map[string]string {
	out := make(map[string]string, len(s.vals))
	for k, v := range s.vals {
		out[k] = v
	}
	return out
}

// ApplyOverrideDir replaces sections from `<section>.md` files in dir; a
// missing directory is a no-op. Callers own trust enforcement for project
// directories (Phase 8).
func (s *Sections) ApplyOverrideDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("prompt: override dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("prompt: read override %s: %w", e.Name(), err)
		}
		s.Set(strings.TrimSuffix(e.Name(), ".md"), string(data))
	}
	return nil
}
