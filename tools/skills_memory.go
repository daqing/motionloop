package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/memory"
	"github.com/daqing/motionloop/skills"
)

// SkillsLoad loads one skill's full SKILL.md on demand (progressive
// disclosure): the index advertises name and description, the model pulls
// the complete instructions only when a task matches.
type SkillsLoad struct {
	// List is the discovered skill set to resolve names against.
	List []skills.Skill
}

type skillsLoadParams struct {
	Name string `json:"name" jsonschema:"required,description=Name of the skill to load"`
}

type skillsLoadDetails struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
}

// Name implements agent.Tool.
func (*SkillsLoad) Name() string { return "skills_load" }

// Description implements agent.Tool.
func (*SkillsLoad) Description() string {
	return "Load one skill's full instructions by name. Read the returned content and follow it for the rest of the session."
}

// Parameters implements agent.Tool.
func (*SkillsLoad) Parameters() *agent.Schema {
	return agent.MustSchemaFor(&skillsLoadParams{})
}

// Execute implements agent.Tool.
func (t *SkillsLoad) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p skillsLoadParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	for _, s := range t.List {
		if s.Name == p.Name {
			data, err := os.ReadFile(s.Path)
			if err != nil {
				return agent.Result{}, fmt.Errorf("load skill %s: %w", p.Name, err)
			}
			return agent.Result{
				Content: []llm.ContentBlock{llm.TextBlock{Text: string(data)}},
				Details: skillsLoadDetails{Name: s.Name, Bytes: len(data)},
			}, nil
		}
	}
	var names []string
	for _, s := range t.List {
		names = append(names, s.Name)
	}
	return agent.Result{}, fmt.Errorf("unknown skill %q; available: %s", p.Name, strings.Join(names, ", "))
}

// MemorySave persists one long-term memory fact for the workspace.
type MemorySave struct {
	Store *memory.Store
	Slug  string
}

type memorySaveParams struct {
	Name        string `json:"name" jsonschema:"required,description=Short kebab-case name for the fact"`
	Description string `json:"description" jsonschema:"required,description=One-line summary shown in the memory index"`
	Type        string `json:"type" jsonschema:"required,description=Fact type: user, feedback, project, or reference"`
	Body        string `json:"body" jsonschema:"description=Full fact content"`
}

type memorySaveDetails struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Name implements agent.Tool.
func (*MemorySave) Name() string { return "memory_save" }

// Description implements agent.Tool.
func (*MemorySave) Description() string {
	return "Save a durable fact to long-term memory (types: user preference, feedback correction, project constraint, reference). Saving again with the same name updates the fact."
}

// Parameters implements agent.Tool.
func (*MemorySave) Parameters() *agent.Schema {
	return agent.MustSchemaFor(&memorySaveParams{})
}

// Execute implements agent.Tool.
func (t *MemorySave) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p memorySaveParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	fact := memory.Fact{Name: p.Name, Description: p.Description, Type: p.Type, Body: p.Body}
	if err := t.Store.Save(t.Slug, fact); err != nil {
		return agent.Result{}, err
	}
	return agent.Result{
		Content: []llm.ContentBlock{llm.TextBlock{Text: fmt.Sprintf("saved memory %q (%s); it will load in future sessions", fact.Name, fact.Type)}},
		Details: memorySaveDetails{Name: fact.Name, Type: fact.Type},
	}, nil
}
