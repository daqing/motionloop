package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// Write writes whole files, refusing to overwrite files the agent has not
// read in this session.
type Write struct{ WS *Workspace }

type writeParams struct {
	Path    string `json:"path" jsonschema:"required,description=File path, relative to the workspace root"`
	Content string `json:"content" jsonschema:"required,description=Complete file content"`
}

type writeDetails struct {
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
	Created bool   `json:"created"`
}

// Name implements agent.Tool.
func (Write) Name() string { return "write" }

// Description implements agent.Tool.
func (Write) Description() string {
	return "Write a file's complete content. Overwriting an existing file requires reading it first in this session."
}

// Parameters implements agent.Tool.
func (Write) Parameters() *agent.Schema { return agent.MustSchemaFor(&writeParams{}) }

// ExecutionMode implements the sequential scheduling override.
func (Write) ExecutionMode() agent.ExecutionMode { return agent.ExecutionSequential }

// Execute implements agent.Tool.
func (t Write) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p writeParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	var details writeDetails
	err := t.WS.Mutate(func() error {
		abs, err := t.WS.resolve(p.Path)
		if err != nil {
			return err
		}
		info, statErr := os.Stat(abs)
		if statErr == nil && info.IsDir() {
			return fmt.Errorf("%s is a directory", p.Path)
		}
		created := statErr != nil
		if !created && !t.WS.hasRead(abs) {
			return fmt.Errorf("refusing to overwrite %s: file has not been read in this session; read it first", p.Path)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
		mode := os.FileMode(0o644)
		if !created {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(abs, []byte(p.Content), mode); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		t.WS.markRead(abs)
		details = writeDetails{Path: p.Path, Bytes: len(p.Content), Created: created}
		return nil
	})
	if err != nil {
		return agent.Result{}, err
	}
	verb := "wrote"
	if details.Created {
		verb = "created"
	}
	return agent.Result{
		Content: []llm.ContentBlock{llm.TextBlock{Text: fmt.Sprintf("%s %s (%d bytes)", verb, p.Path, details.Bytes)}},
		Details: details,
	}, nil
}
