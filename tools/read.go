package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

// defaultReadLines caps the lines returned by one Read call.
const defaultReadLines = 2000

// Read returns line-numbered file contents, or an image block for known
// image types.
type Read struct {
	// Root is the workspace root; relative paths resolve against it.
	Root string
}

type readParams struct {
	Path   string `json:"path" jsonschema:"required,description=File path, relative to the workspace root"`
	Offset int    `json:"offset" jsonschema:"description=1-based line to start from"`
	Limit  int    `json:"limit" jsonschema:"description=Maximum lines to return, default 2000"`
}

type readDetails struct {
	Path       string `json:"path"`
	TotalLines int    `json:"totalLines"`
	ShownLines int    `json:"shownLines"`
	Truncated  bool   `json:"truncated,omitempty"`
	Image      bool   `json:"image,omitempty"`
}

var imageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// Name implements agent.Tool.
func (Read) Name() string { return "read" }

// Description implements agent.Tool.
func (Read) Description() string {
	return "Read a file. Lines are prefixed with 1-based line numbers. Returns an image block for known image types."
}

// Parameters implements agent.Tool.
func (Read) Parameters() *agent.Schema {
	return agent.MustSchemaFor(&readParams{})
}

// Execute implements agent.Tool.
func (r Read) Execute(ctx context.Context, call agent.ToolCall, emit func(agent.Update)) (agent.Result, error) {
	var p readParams
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &p); err != nil {
			return agent.Result{}, fmt.Errorf("parse arguments: %w", err)
		}
	}
	path := p.Path
	if !filepath.IsAbs(path) && r.Root != "" {
		path = filepath.Join(r.Root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return agent.Result{}, fmt.Errorf("read %s: %w", p.Path, err)
	}

	if mime, ok := imageTypes[strings.ToLower(filepath.Ext(path))]; ok {
		return agent.Result{
			Content: []llm.ContentBlock{llm.ImageBlock{
				Data:     base64.StdEncoding.EncodeToString(data),
				MimeType: mime,
			}},
			Details: readDetails{Path: p.Path, Image: true},
		}, nil
	}

	lines := strings.Split(string(data), "\n")
	total := len(lines)
	offset := 1
	if p.Offset > 1 {
		offset = p.Offset
	}
	limit := defaultReadLines
	if p.Limit > 0 {
		limit = p.Limit
	}
	if offset > total {
		return agent.Result{
			Content: []llm.ContentBlock{llm.TextBlock{Text: fmt.Sprintf("file has %d lines; offset %d is past the end", total, offset)}},
			Details: readDetails{Path: p.Path, TotalLines: total},
		}, nil
	}
	end := offset - 1 + limit
	if end > total {
		end = total
	}
	var sb strings.Builder
	for i := offset - 1; i < end; i++ {
		fmt.Fprintf(&sb, "%d\t%s\n", i+1, lines[i])
	}
	truncated := end < total
	if truncated {
		fmt.Fprintf(&sb, "... (%d more lines below; use offset=%d to continue)\n", total-end, end+1)
	}
	return agent.Result{
		Content: []llm.ContentBlock{llm.TextBlock{Text: sb.String()}},
		Details: readDetails{Path: p.Path, TotalLines: total, ShownLines: end - offset + 1, Truncated: truncated},
	}, nil
}
