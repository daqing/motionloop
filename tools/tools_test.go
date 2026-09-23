package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

func testWorkspace(t *testing.T) *Workspace {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func execTool(t *testing.T, tool agent.Tool, args string) (agent.Result, error) {
	t.Helper()
	return tool.Execute(context.Background(),
		agent.ToolCall{ID: "c1", Name: tool.Name(), Arguments: json.RawMessage(args)}, func(agent.Update) {})
}

// --- write ---

func TestWriteCreateNested(t *testing.T) {
	ws := testWorkspace(t)
	res, err := execTool(t, Write{WS: ws}, `{"path":"a/b/c.txt","content":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(ws.Root, "a/b/c.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("file = %q, %v", got, err)
	}
	d, ok := res.Details.(writeDetails)
	if !ok || !d.Created || d.Bytes != 5 {
		t.Fatalf("details = %+v", res.Details)
	}
}

func TestWriteOverwriteRequiresRead(t *testing.T) {
	ws := testWorkspace(t)
	if _, err := execTool(t, Write{WS: ws}, `{"path":"f.txt","content":"v1"}`); err != nil {
		t.Fatal(err)
	}
	// same session: write marked it read, overwrite is allowed
	if _, err := execTool(t, Write{WS: ws}, `{"path":"f.txt","content":"v2"}`); err != nil {
		t.Fatal(err)
	}

	// fresh session: file exists but was never read
	fresh, err := NewWorkspace(filepath.Dir(ws.Root))
	if err != nil {
		t.Fatal(err)
	}
	_, err = execTool(t, Write{WS: fresh}, `{"path":"`+filepath.Base(ws.Root)+`/f.txt","content":"v3"}`)
	if err == nil || !strings.Contains(err.Error(), "has not been read") {
		t.Fatalf("err = %v, want not-read refusal", err)
	}
	// after a read in the fresh session, overwrite succeeds
	if _, err := execTool(t, Read{WS: fresh}, `{"path":"`+filepath.Base(ws.Root)+`/f.txt","limit":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := execTool(t, Write{WS: fresh}, `{"path":"`+filepath.Base(ws.Root)+`/f.txt","content":"v3"}`); err != nil {
		t.Fatal(err)
	}
}

func TestWriteDirectoryRefused(t *testing.T) {
	ws := testWorkspace(t)
	_, err := execTool(t, Write{WS: ws}, `{"path":".","content":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("err = %v", err)
	}
}

// --- edit ---

func editFile(t *testing.T, ws *Workspace, content string) string {
	t.Helper()
	if _, err := execTool(t, Write{WS: ws}, `{"path":"e.go","content":`+quoteJSON(content)+`}`); err != nil {
		t.Fatal(err)
	}
	return "e.go"
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestEditReplace(t *testing.T) {
	ws := testWorkspace(t)
	editFile(t, ws, "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n")
	res, err := execTool(t, Edit{WS: ws}, `{"path":"e.go","old_string":"\"hi\"","new_string":"\"hello\""}`)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ws.Root, "e.go"))
	if !strings.Contains(string(got), `"hello"`) || strings.Contains(string(got), `"hi"`) {
		t.Fatalf("content = %q", got)
	}
	var diff string
	for _, b := range res.Content {
		if tb, ok := b.(llm.TextBlock); ok {
			diff = tb.Text
		}
	}
	if !strings.Contains(diff, "@@ e.go:4 @@") || !strings.Contains(diff, "+\tprintln(\"hello\")") {
		t.Fatalf("diff = %q", diff)
	}
}

func TestEditNotFound(t *testing.T) {
	ws := testWorkspace(t)
	editFile(t, ws, "one\ntwo\n")
	_, err := execTool(t, Edit{WS: ws}, `{"path":"e.go","old_string":"three","new_string":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestEditAmbiguousListsLines(t *testing.T) {
	ws := testWorkspace(t)
	editFile(t, ws, "alpha\nbeta\nalpha\ngamma\nalpha\n")
	_, err := execTool(t, Edit{WS: ws}, `{"path":"e.go","old_string":"alpha","new_string":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "3 locations") || !strings.Contains(err.Error(), "lines 1, 3, 5") {
		t.Fatalf("err = %v", err)
	}
}

func TestEditAfterWrite(t *testing.T) {
	ws := testWorkspace(t)
	editFile(t, ws, "hello\n")
	if _, err := execTool(t, Edit{WS: ws}, `{"path":"e.go","old_string":"hello","new_string":"bye"}`); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(ws.Root, "e.go")); string(b) != "bye\n" && string(b) != "bye" {
		t.Fatalf("content = %q", b)
	}
}

func TestEditUnreadSessionRefused(t *testing.T) {
	ws := testWorkspace(t)
	editFile(t, ws, "hello\n")

	// a second workspace anchored at the same root has no read history
	second, err := NewWorkspace(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = execTool(t, Edit{WS: second}, `{"path":"e.go","old_string":"hello","new_string":"bye"}`)
	if err == nil || !strings.Contains(err.Error(), "not been read") {
		t.Fatalf("err = %v", err)
	}
}

func TestPathEscapeRejected(t *testing.T) {
	ws := testWorkspace(t)
	outside := filepath.Join(filepath.Dir(ws.Root), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	for _, tc := range []struct {
		tool agent.Tool
		args string
	}{
		{Read{WS: ws}, `{"path":"../outside.txt"}`},
		{Write{WS: ws}, `{"path":"../evil.txt","content":"x"}`},
		{Edit{WS: ws}, `{"path":"../outside.txt","old_string":"a","new_string":"b"}`},
		{Grep{WS: ws, DisableRipgrep: true}, `{"pattern":"x","path":".."}`},
		{Glob{WS: ws}, `{"pattern":"*","path":".."}`},
		{Ls{WS: ws}, `{"path":".."}`},
	} {
		_, err := execTool(t, tc.tool, tc.args)
		if err == nil || !strings.Contains(err.Error(), "escapes the workspace") {
			t.Errorf("%s: err = %v, want escape refusal", tc.tool.Name(), err)
		}
	}
}

// --- grep ---

func grepTree(t *testing.T) *Workspace {
	t.Helper()
	ws := testWorkspace(t)
	mkdir(t, ws.Root, "sub")
	writeFile(t, ws.Root, "a.go", "alpha\nbeta\n")
	writeFile(t, ws.Root, "notes.txt", "alpha here\n")
	writeFile(t, ws.Root, "sub/b.go", "alpha\ndelta\n")
	mkdir(t, ws.Root, ".hidden")
	writeFile(t, ws.Root, ".hidden/h.go", "alpha\n")
	return ws
}

func TestGrepBuiltin(t *testing.T) {
	ws := grepTree(t)
	res, err := execTool(t, Grep{WS: ws, DisableRipgrep: true}, `{"pattern":"alpha","glob":"**/*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	text := mainText(res)
	for _, want := range []string{"a.go:1:alpha", "sub/b.go:1:alpha"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "notes.txt") || strings.Contains(text, ".hidden") {
		t.Fatalf("unexpected matches:\n%s", text)
	}
	d, ok := res.Details.(grepDetails)
	if !ok || d.Matches != 2 || d.Files != 2 || d.UsedRipgrep {
		t.Fatalf("details = %+v", res.Details)
	}
}

func TestGrepScopedPath(t *testing.T) {
	ws := grepTree(t)
	res, err := execTool(t, Grep{WS: ws, DisableRipgrep: true}, `{"pattern":"alpha","path":"sub"}`)
	if err != nil {
		t.Fatal(err)
	}
	text := mainText(res)
	if !strings.Contains(text, "b.go:1:alpha") || strings.Contains(text, "a.go") {
		t.Fatalf("output = %q", text)
	}
}

func TestGrepNoMatches(t *testing.T) {
	ws := grepTree(t)
	res, err := execTool(t, Grep{WS: ws, DisableRipgrep: true}, `{"pattern":"zzz-nothing"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainText(res), "No matches") {
		t.Fatalf("output = %q", mainText(res))
	}
}

func TestGrepInvalidPattern(t *testing.T) {
	ws := grepTree(t)
	_, err := execTool(t, Grep{WS: ws, DisableRipgrep: true}, `{"pattern":"[oops"}`)
	if err == nil || !strings.Contains(err.Error(), "invalid pattern") {
		t.Fatalf("err = %v", err)
	}
}

func TestGrepRipgrepWhenAvailable(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
	ws := grepTree(t)
	res, err := execTool(t, Grep{WS: ws}, `{"pattern":"alpha","glob":"**/*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := res.Details.(grepDetails)
	if !ok || !d.UsedRipgrep || d.Matches != 2 {
		t.Fatalf("details = %+v, output = %q", res.Details, mainText(res))
	}
}

// --- glob ---

func TestGlobDoubleStar(t *testing.T) {
	ws := grepTree(t)
	res, err := execTool(t, Glob{WS: ws}, `{"pattern":"**/*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "sub/b.go"}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(mainText(res)), "\n") {
		if line != "" {
			got = append(got, line)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("glob = %v, want %v", got, want)
	}
}

func TestGlobSingleStarStaysInDirectory(t *testing.T) {
	ws := grepTree(t)
	res, err := execTool(t, Glob{WS: ws}, `{"pattern":"*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(mainText(res)); got != "a.go" {
		t.Fatalf("glob = %q", got)
	}
}

func TestGlobNoMatches(t *testing.T) {
	ws := grepTree(t)
	res, err := execTool(t, Glob{WS: ws}, `{"pattern":"**/*.rs"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainText(res), "No files matched") {
		t.Fatalf("output = %q", mainText(res))
	}
}

// --- ls ---

func TestLsMarkers(t *testing.T) {
	ws := testWorkspace(t)
	mkdir(t, ws.Root, "sub")
	writeFile(t, ws.Root, "f.txt", "x")
	writeFile(t, ws.Root, ".dot", "x")
	if err := os.Symlink("f.txt", filepath.Join(ws.Root, "link")); err != nil {
		t.Fatal(err)
	}

	res, err := execTool(t, Ls{WS: ws}, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	text := mainText(res)
	for _, want := range []string{"- f.txt", "d sub", "l link -> f.txt"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, ".dot") {
		t.Fatalf("hidden entry leaked:\n%s", text)
	}

	res, err = execTool(t, Ls{WS: ws}, `{"include_hidden":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainText(res), ".dot") {
		t.Fatalf("hidden entry missing with include_hidden:\n%s", mainText(res))
	}
}

// --- truncate ---

func TestTruncateOutput(t *testing.T) {
	short := "hello"
	if got, truncated := truncateOutput(short, 100); got != short || truncated {
		t.Fatalf("short: %q %v", got, truncated)
	}
	long := strings.Repeat("a", 6000) + "TAILMARK" + strings.Repeat("b", 4000)
	got, truncated := truncateOutput(long, 9000)
	if !truncated {
		t.Fatal("not truncated")
	}
	if !strings.HasPrefix(got, "aaaa") || !strings.HasSuffix(got, "bbbb") {
		t.Fatal("head/tail not preserved")
	}
	if !strings.Contains(got, "bytes truncated") {
		t.Fatal("marker missing")
	}
}

// --- mutation queue ---

func TestMutateSerializes(t *testing.T) {
	ws := testWorkspace(t)
	var current, maxSeen atomic.Int32
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			_ = ws.Mutate(func() error {
				n := current.Add(1)
				for {
					m := maxSeen.Load()
					if n <= m || maxSeen.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				current.Add(-1)
				return nil
			})
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if maxSeen.Load() != 1 {
		t.Fatalf("max concurrent mutations = %d, want 1", maxSeen.Load())
	}
}

func TestWriteEditSequentialMode(t *testing.T) {
	ws := testWorkspace(t)
	for _, tool := range []agent.Tool{Write{WS: ws}, Edit{WS: ws}} {
		m, ok := tool.(interface{ ExecutionMode() agent.ExecutionMode })
		if !ok || m.ExecutionMode() != agent.ExecutionSequential {
			t.Fatalf("%s is not sequential", tool.Name())
		}
	}
}

// --- registry ---

func TestPresets(t *testing.T) {
	ws := testWorkspace(t)
	names := func(ts []agent.Tool) []string {
		out := make([]string, len(ts))
		for i, t := range ts {
			out[i] = t.Name()
		}
		return out
	}
	if got, want := names(Coding(ws)), []string{"bash", "read", "write", "edit", "grep", "glob", "ls"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("coding preset = %v, want %v", got, want)
	}
	if got, want := names(Minimal(ws)), []string{"bash", "read", "write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("minimal preset = %v, want %v", got, want)
	}
}

// --- helpers ---

func mkdir(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
