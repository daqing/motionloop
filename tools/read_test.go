package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
)

func readWith(t *testing.T, root, args string) agent.Result {
	t.Helper()
	res, err := Read{Root: root}.Execute(context.Background(),
		agent.ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(args)}, func(agent.Update) {})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return res
}

func mainText(res agent.Result) string {
	for _, b := range res.Content {
		if tb, ok := b.(llm.TextBlock); ok {
			return tb.Text
		}
	}
	return ""
}

func TestReadLineNumbers(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "f.txt", "a\nb\nc")

	res := readWith(t, dir, `{"path":"f.txt"}`)
	want := "1\ta\n2\tb\n3\tc\n"
	if got := mainText(res); got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	d, ok := res.Details.(readDetails)
	if !ok || d.TotalLines != 3 || d.ShownLines != 3 || d.Truncated {
		t.Fatalf("details = %+v", res.Details)
	}
}

func TestReadOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "f.txt", "a\nb\nc\nd")

	res := readWith(t, dir, `{"path":"f.txt","offset":2,"limit":2}`)
	want := "2\tb\n3\tc\n... (1 more lines below; use offset=4 to continue)\n"
	if got := mainText(res); got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	d, ok := res.Details.(readDetails)
	if !ok || !d.Truncated {
		t.Fatalf("details = %+v, want truncated", res.Details)
	}
	if got := mainText(res); !strings.Contains(got, "use offset=4") {
		t.Fatalf("content missing continuation hint: %q", got)
	}
}

func TestReadOffsetPastEnd(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "f.txt", "a\nb")

	res := readWith(t, dir, `{"path":"f.txt","offset":99}`)
	if got := mainText(res); !strings.Contains(got, "file has 2 lines") {
		t.Fatalf("content = %q", got)
	}
}

func TestReadMissingFile(t *testing.T) {
	_, err := Read{Root: t.TempDir()}.Execute(context.Background(),
		agent.ToolCall{ID: "c", Name: "read", Arguments: json.RawMessage(`{"path":"nope.txt"}`)}, func(agent.Update) {})
	if err == nil || !strings.Contains(err.Error(), "nope.txt") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadImage(t *testing.T) {
	dir := t.TempDir()
	raw := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	res := readWith(t, dir, `{"path":"pic.png"}`)
	if len(res.Content) != 1 {
		t.Fatalf("content blocks = %d", len(res.Content))
	}
	img, ok := res.Content[0].(llm.ImageBlock)
	if !ok {
		t.Fatalf("block = %#v, want ImageBlock", res.Content[0])
	}
	if img.MimeType != "image/png" {
		t.Fatalf("mimeType = %q", img.MimeType)
	}
	if img.Data != base64.StdEncoding.EncodeToString(raw) {
		t.Fatalf("data mismatch")
	}
	d, ok := res.Details.(readDetails)
	if !ok || !d.Image {
		t.Fatalf("details = %+v", res.Details)
	}
}

func TestReadNestedRelativePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "sub/f.txt", "deep")

	if got := mainText(readWith(t, dir, `{"path":"sub/f.txt"}`)); got != "1\tdeep\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestReadSchema(t *testing.T) {
	s := Read{}.Parameters()
	if s.Properties["path"] == nil || len(s.Required) != 1 || s.Required[0] != "path" {
		t.Fatalf("schema = %+v", s)
	}
	if err := agent.Validate(s, json.RawMessage(`{"path":123}`)); err == nil {
		t.Fatal("validation should reject non-string path")
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
