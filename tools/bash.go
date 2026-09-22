// Package tools ships motionloop's built-in tool set. Tools depend only on
// the agent package's Tool interface.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// Bash executes shell commands and returns their combined output.
type Bash struct{}

type bashParams struct {
	Command string `json:"command" jsonschema:"required,description=Shell command to execute"`
	Workdir string `json:"workdir" jsonschema:"description=Working directory, defaults to the current directory"`
	Timeout int    `json:"timeout" jsonschema:"description=Timeout in seconds, default 30"`
}

type bashDetails struct {
	ExitCode  int  `json:"exitCode"`
	TimedOut  bool `json:"timedOut,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
}

// bashMaxOutput caps the content returned to the model, in bytes.
const bashMaxOutput = 64 * 1024

// Name implements agent.Tool.
func (Bash) Name() string { return "bash" }

// Description implements agent.Tool.
func (Bash) Description() string {
	return "Execute a shell command and return its combined stdout/stderr. Non-zero exit codes are reported in the output; the assistant can react to them."
}

// Parameters implements agent.Tool.
func (Bash) Parameters() *agent.Schema {
	return agent.MustSchemaFor(&bashParams{})
}

// Execute implements agent.Tool.
func (Bash) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p bashParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	timeout := 30 * time.Second
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", p.Command)
	if p.Workdir != "" {
		cmd.Dir = p.Workdir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return agent.Result{}, fmt.Errorf("run command: %w", err)
		}
	}

	exitCode := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
	}
	output := string(out)
	truncated := false
	if len(output) > bashMaxOutput {
		output = output[:bashMaxOutput] + "\n... [output truncated]"
		truncated = true
	}
	var sb strings.Builder
	sb.WriteString(output)
	if runCtx.Err() == context.DeadlineExceeded {
		fmt.Fprintf(&sb, "\ncommand timed out after %s", timeout)
	}
	if exitCode != 0 {
		fmt.Fprintf(&sb, "\nExit code: %d", exitCode)
	}
	return agent.Result{
		Content: []llm.ContentBlock{llm.TextBlock{Text: strings.TrimSpace(sb.String())}},
		Details: bashDetails{ExitCode: exitCode, TimedOut: runCtx.Err() == context.DeadlineExceeded, Truncated: truncated},
	}, nil
}
