package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// Edit replaces one exact string occurrence in a file the agent has read.
type Edit struct{ WS *Workspace }

type editParams struct {
	Path      string `json:"path" jsonschema:"required,description=File path, relative to the workspace root"`
	OldString string `json:"old_string" jsonschema:"required,description=Exact text to replace; must occur exactly once"`
	NewString string `json:"new_string" jsonschema:"required,description=Replacement text"`
}

type editDetails struct {
	Path     string `json:"path"`
	OldLines int    `json:"oldLines"`
	NewLines int    `json:"newLines"`
}

const editDiffMaxBytes = 8 * 1024

// Name implements agent.Tool.
func (Edit) Name() string { return "edit" }

// Description implements agent.Tool.
func (Edit) Description() string {
	return "Replace an exact string in a file. The old text must match exactly one location; include surrounding lines when it appears multiple times. The file must have been read in this session."
}

// Parameters implements agent.Tool.
func (Edit) Parameters() *agent.Schema { return agent.MustSchemaFor(&editParams{}) }

// ExecutionMode implements the sequential scheduling override.
func (Edit) ExecutionMode() agent.ExecutionMode { return agent.ExecutionSequential }

// Execute implements agent.Tool.
func (t Edit) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p editParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	var diff string
	var details editDetails
	err := t.WS.Mutate(func() error {
		abs, err := t.WS.resolve(p.Path)
		if err != nil {
			return err
		}
		if !t.WS.hasRead(abs) {
			return fmt.Errorf("%s has not been read in this session; read it first", p.Path)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Errorf("stat %s: %w", p.Path, err)
		}
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", p.Path)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("read %s: %w", p.Path, err)
		}
		before := string(data)
		count := strings.Count(before, p.OldString)
		switch {
		case count == 0:
			return fmt.Errorf("old_string not found in %s", p.Path)
		case count > 1:
			return fmt.Errorf("old_string matches %d locations in %s (lines %s); include more surrounding context to make it unique",
				count, p.Path, joinInts(matchLines(before, p.OldString)))
		}
		idx := strings.Index(before, p.OldString)
		after := before[:idx] + p.NewString + before[idx+len(p.OldString):]
		if err := os.WriteFile(abs, []byte(after), info.Mode().Perm()); err != nil {
			return fmt.Errorf("write %s: %w", p.Path, err)
		}
		details = editDetails{
			Path:     p.Path,
			OldLines: countLines(p.OldString),
			NewLines: countLines(p.NewString),
		}
		diff, _ = truncateOutput(editDiff(p.Path, before, after, idx, len(p.OldString), len(p.NewString)), editDiffMaxBytes)
		return nil
	})
	if err != nil {
		return agent.Result{}, err
	}
	return agent.Result{
		Content: []llm.ContentBlock{llm.TextBlock{Text: diff}},
		Details: details,
	}, nil
}

// matchLines lists 1-based line numbers of every occurrence of sub in s.
func matchLines(s, sub string) []int {
	var lines []int
	line := 1
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], sub)
		if idx < 0 {
			line += strings.Count(s[i:], "\n")
			break
		}
		line += strings.Count(s[i:i+idx], "\n")
		lines = append(lines, line)
		i = i + idx + len(sub)
		line += strings.Count(sub, "\n")
	}
	return lines
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return 1 + strings.Count(s, "\n")
}

// editDiff renders a small unified-style hunk around one replacement.
func editDiff(path, before, after string, idx, oldLen, newLen int) string {
	startLine := 1 + strings.Count(before[:idx], "\n")
	lastOld := 1 + strings.Count(before[:idx+oldLen-1], "\n")
	lastNew := 1 + strings.Count(after[:idx+newLen-1], "\n")

	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	const ctx = 2

	var sb strings.Builder
	fmt.Fprintf(&sb, "@@ %s:%d @@\n", path, startLine)
	for i := startLine - 1 - ctx; i < startLine-1; i++ {
		if i >= 0 && i < len(beforeLines) {
			sb.WriteString(" " + beforeLines[i] + "\n")
		}
	}
	for i := startLine - 1; i < lastOld && i < len(beforeLines); i++ {
		sb.WriteString("-" + beforeLines[i] + "\n")
	}
	for i := startLine - 1; i < lastNew && i < len(afterLines); i++ {
		sb.WriteString("+" + afterLines[i] + "\n")
	}
	for i := lastNew; i < lastNew+ctx && i < len(afterLines); i++ {
		sb.WriteString(" " + afterLines[i] + "\n")
	}
	return sb.String()
}

func joinInts(vals []int) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}
