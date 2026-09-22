package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// Grep searches file contents with a regular expression, delegating to
// system ripgrep when available and falling back to a built-in walker.
type Grep struct {
	WS *Workspace
	// DisableRipgrep forces the built-in walker (tests, minimal hosts).
	DisableRipgrep bool
}

type grepParams struct {
	Pattern string `json:"pattern" jsonschema:"required,description=Regular expression to search for"`
	Path    string `json:"path" jsonschema:"description=Directory or file to search, defaults to the workspace root"`
	Glob    string `json:"glob" jsonschema:"description=Restrict the search to files matching this glob"`
}

type grepDetails struct {
	Matches     int  `json:"matches"`
	Files       int  `json:"files"`
	UsedRipgrep bool `json:"usedRipgrep"`
	Truncated   bool `json:"truncated"`
}

const (
	grepMaxLines  = 500
	grepMaxOutput = 30 * 1024
	grepMaxFile   = 10 * 1024 * 1024
)

// Name implements agent.Tool.
func (Grep) Name() string { return "grep" }

// Description implements agent.Tool.
func (Grep) Description() string {
	return "Search file contents with a regular expression. Results are path:line:text, one per match."
}

// Parameters implements agent.Tool.
func (Grep) Parameters() *agent.Schema { return agent.MustSchemaFor(&grepParams{}) }

// Execute implements agent.Tool.
func (t Grep) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p grepParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	if _, err := regexp.Compile(p.Pattern); err != nil {
		return agent.Result{}, fmt.Errorf("invalid pattern: %w", err)
	}
	base, err := t.WS.resolve(p.Path)
	if err != nil {
		return agent.Result{}, err
	}

	var matches []string
	var usedRipgrep bool
	if !t.DisableRipgrep {
		matches, usedRipgrep, err = grepRipgrep(ctx, p, base)
	}
	if !usedRipgrep {
		matches, err = grepBuiltin(p, base)
	}
	if err != nil {
		return agent.Result{}, err
	}

	truncatedLines := false
	if len(matches) > grepMaxLines {
		matches = matches[:grepMaxLines]
		truncatedLines = true
	}
	files := map[string]bool{}
	for _, m := range matches {
		if i := strings.Index(m, ":"); i >= 0 {
			files[m[:i]] = true
		}
	}
	if len(matches) == 0 {
		return agent.Result{
			Content: []llm.ContentBlock{llm.TextBlock{Text: fmt.Sprintf("No matches found for /%s/", p.Pattern)}},
			Details: grepDetails{UsedRipgrep: usedRipgrep},
		}, nil
	}
	out, truncatedBytes := truncateOutput(strings.Join(matches, "\n")+"\n", grepMaxOutput)
	return agent.Result{
		Content: []llm.ContentBlock{llm.TextBlock{Text: out}},
		Details: grepDetails{
			Matches:     len(matches),
			Files:       len(files),
			UsedRipgrep: usedRipgrep,
			Truncated:   truncatedLines || truncatedBytes,
		},
	}, nil
}

func grepRipgrep(ctx context.Context, p grepParams, base string) (matches []string, used bool, err error) {
	rg, lookErr := exec.LookPath("rg")
	if lookErr != nil {
		return nil, false, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	args := []string{"--line-number", "--no-heading", "--color", "never"}
	if p.Glob != "" {
		args = append(args, "--glob", p.Glob)
	}
	args = append(args, "-e", p.Pattern, ".")
	cmd := exec.CommandContext(runCtx, rg, args...)
	cmd.Dir = base
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, true, nil // no matches
		}
		return nil, false, nil // unusable rg; fall back to the built-in walker
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			matches = append(matches, line)
		}
	}
	return matches, true, nil
}

func grepBuiltin(p grepParams, base string) ([]string, error) {
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return nil, err
	}
	var glob *regexp.Regexp
	if p.Glob != "" {
		glob, err = globToRegexp(p.Glob)
		if err != nil {
			return nil, err
		}
	}
	var matches []string
	info, err := os.Stat(base)
	if err != nil {
		return nil, fmt.Errorf("search path: %w", err)
	}
	if !info.IsDir() {
		return grepFile(re, base, base), nil
	}
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != base && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(path, base), string(filepath.Separator)))
		if glob != nil && !glob.MatchString(rel) {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.Size() > grepMaxFile {
			return nil
		}
		matches = append(matches, grepFile(re, path, rel)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func grepFile(re *regexp.Regexp, abs, display string) []string {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	var out []string
	for i, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			out = append(out, fmt.Sprintf("%s:%d:%s", display, i+1, line))
		}
	}
	return out
}
