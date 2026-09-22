package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// Glob lists workspace files matching a pattern; ** crosses directory
// boundaries, * stays within one.
type Glob struct{ WS *Workspace }

type globParams struct {
	Pattern string `json:"pattern" jsonschema:"required,description=Glob pattern; * matches within one directory, ** across directories"`
	Path    string `json:"path" jsonschema:"description=Directory to search under, defaults to the workspace root"`
}

type globDetails struct {
	Matches   int  `json:"matches"`
	Truncated bool `json:"truncated"`
}

const globMaxResults = 1000

// Name implements agent.Tool.
func (Glob) Name() string { return "glob" }

// Description implements agent.Tool.
func (Glob) Description() string {
	return "List files matching a glob pattern, sorted, one path per line. Also serves as find: use ** to search recursively."
}

// Parameters implements agent.Tool.
func (Glob) Parameters() *agent.Schema { return agent.MustSchemaFor(&globParams{}) }

// Execute implements agent.Tool.
func (t Glob) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p globParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	base, err := t.WS.resolve(p.Path)
	if err != nil {
		return agent.Result{}, err
	}
	re, err := globToRegexp(p.Pattern)
	if err != nil {
		return agent.Result{}, err
	}
	var matches []string
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
		if re.MatchString(rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return agent.Result{}, err
	}
	sort.Strings(matches)
	truncated := false
	if len(matches) > globMaxResults {
		matches = matches[:globMaxResults]
		truncated = true
	}
	if len(matches) == 0 {
		return agent.Result{
			Content: []llm.ContentBlock{llm.TextBlock{Text: fmt.Sprintf("No files matched %q", p.Pattern)}},
			Details: globDetails{},
		}, nil
	}
	return agent.Result{
		Content: []llm.ContentBlock{llm.TextBlock{Text: strings.Join(matches, "\n") + "\n"}},
		Details: globDetails{Matches: len(matches), Truncated: truncated},
	}, nil
}

// globToRegexp compiles a glob with ** support into an anchored regexp.
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					sb.WriteString("(?:.*/)?")
					i += 2
				} else {
					sb.WriteString(".*")
					i++
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '.', '(', ')', '[', ']', '{', '}', '+', '^', '$', '|', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}
