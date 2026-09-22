package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

func bashResult(t *testing.T, args string) agent.Result {
	t.Helper()
	res, err := Bash{}.Execute(context.Background(), agent.ToolCall{ID: "c1", Name: "bash", Arguments: json.RawMessage(args)}, func(agent.Update) {})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return res
}

func resultText(res agent.Result) string {
	for _, b := range res.Content {
		if tb, ok := b.(llm.TextBlock); ok {
			return tb.Text
		}
	}
	return ""
}

func TestBashEcho(t *testing.T) {
	res := bashResult(t, `{"command":"echo hello"}`)
	if got := resultText(res); got != "hello" {
		t.Fatalf("output = %q", got)
	}
	details, ok := res.Details.(bashDetails)
	if !ok || details.ExitCode != 0 {
		t.Fatalf("details = %+v", res.Details)
	}
}

func TestBashWorkdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("from-workdir"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := bashResult(t, jsonOrDie(t, map[string]any{"command": "cat note.txt", "workdir": dir}))
	if got := resultText(res); got != "from-workdir" {
		t.Fatalf("output = %q", got)
	}
}

func TestBashNonZeroExit(t *testing.T) {
	res := bashResult(t, `{"command":"echo oops >&2; exit 3"}`)
	got := resultText(res)
	if !strings.Contains(got, "oops") || !strings.Contains(got, "Exit code: 3") {
		t.Fatalf("output = %q", got)
	}
	if d, ok := res.Details.(bashDetails); !ok || d.ExitCode != 3 {
		t.Fatalf("details = %+v", res.Details)
	}
}

func TestBashTimeout(t *testing.T) {
	start := time.Now()
	res := bashResult(t, `{"command":"sleep 5","timeout":1}`)
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("timeout not enforced, took %s", elapsed)
	}
	got := resultText(res)
	if !strings.Contains(got, "timed out") {
		t.Fatalf("output = %q", got)
	}
	if d, ok := res.Details.(bashDetails); !ok || !d.TimedOut {
		t.Fatalf("details = %+v", res.Details)
	}
}

func TestBashBadArgumentsJSON(t *testing.T) {
	// schema validation normally rejects this earlier; the tool still
	// guards its own unmarshal path
	_, err := Bash{}.Execute(context.Background(), agent.ToolCall{ID: "c", Name: "bash", Arguments: json.RawMessage(`{"command":42}`)}, func(agent.Update) {})
	if err == nil || !strings.Contains(err.Error(), "parse arguments") {
		t.Fatalf("err = %v", err)
	}
}

func jsonOrDie(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
