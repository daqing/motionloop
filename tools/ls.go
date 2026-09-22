package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// Ls lists one directory with type markers; hidden entries are skipped by
// default.
type Ls struct{ WS *Workspace }

type lsParams struct {
	Path          string `json:"path" jsonschema:"description=Directory to list, defaults to the workspace root"`
	IncludeHidden bool   `json:"include_hidden" jsonschema:"description=Include entries starting with a dot"`
}

type lsDetails struct {
	Entries int `json:"entries"`
}

// Name implements agent.Tool.
func (Ls) Name() string { return "ls" }

// Description implements agent.Tool.
func (Ls) Description() string {
	return "List a directory: one entry per line prefixed by type marker (d directory, - file, l symlink with target). Hidden entries are skipped unless requested."
}

// Parameters implements agent.Tool.
func (Ls) Parameters() *agent.Schema { return agent.MustSchemaFor(&lsParams{}) }

// Execute implements agent.Tool.
func (t Ls) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p lsParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	dir, err := t.WS.resolve(p.Path)
	if err != nil {
		return agent.Result{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return agent.Result{}, fmt.Errorf("list %s: %w", p.Path, err)
	}
	names := make([]string, 0, len(entries))
	byName := make(map[string]string, len(entries))
	for _, e := range entries {
		if !p.IncludeHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		marker := "-"
		switch {
		case e.Type().IsDir():
			marker = "d"
		case e.Type()&os.ModeSymlink != 0:
			marker = "l"
		case !e.Type().IsRegular():
			marker = "?"
		}
		names = append(names, e.Name())
		byName[e.Name()] = marker
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		if byName[name] == "l" {
			target, err := os.Readlink(dir + string(os.PathSeparator) + name)
			if err != nil {
				target = "?"
			}
			fmt.Fprintf(&sb, "l %s -> %s\n", name, target)
			continue
		}
		fmt.Fprintf(&sb, "%s %s\n", byName[name], name)
	}
	return agent.Result{
		Content: []llm.ContentBlock{llm.TextBlock{Text: sb.String()}},
		Details: lsDetails{Entries: len(names)},
	}, nil
}
