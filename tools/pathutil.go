package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// Workspace anchors the file tools to one root directory: relative paths
// resolve against it, paths outside it are rejected, and it tracks which
// files the agent has seen so writes can refuse blind overwrites. Bash is
// intentionally not anchored.
//
// Escape detection is lexical; symlink targets are not resolved.
type Workspace struct {
	Root string

	mu   sync.Mutex
	read map[string]bool

	mutMu sync.Mutex
}

// NewWorkspace anchors a workspace at root, made absolute.
func NewWorkspace(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("tools: workspace root: %w", err)
	}
	return &Workspace{Root: abs, read: map[string]bool{}}, nil
}

// resolve turns a tool path argument into an absolute path inside the
// workspace. An empty name is the root itself.
func (w *Workspace) resolve(name string) (string, error) {
	if name == "" {
		return w.Root, nil
	}
	var abs string
	if filepath.IsAbs(name) {
		abs = filepath.Clean(name)
	} else {
		abs = filepath.Clean(filepath.Join(w.Root, name))
	}
	if abs != w.Root && !strings.HasPrefix(abs, w.Root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace root", name)
	}
	return abs, nil
}

// rel displays an absolute path relative to the workspace root.
func (w *Workspace) rel(abs string) string {
	rel, err := filepath.Rel(w.Root, abs)
	if err != nil {
		return abs
	}
	return rel
}

// markRead records that the agent has seen the file's current content.
func (w *Workspace) markRead(abs string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.read[abs] = true
}

// hasRead reports whether the file was read (or freshly written) in this
// session.
func (w *Workspace) hasRead(abs string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.read[abs]
}
