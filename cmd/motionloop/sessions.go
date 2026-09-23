package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/session"
)

func sessionsRoot(home string) string {
	return filepath.Join(home, ".motionloop", "sessions")
}

func runSessionCommand(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: motionloop session ls | show <id> | fork <id> | resume <id> -p <prompt>")
		os.Exit(2)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := sessionsRoot(home)

	switch args[0] {
	case "ls":
		return listSessions(root)
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("session show needs an id")
		}
		path, err := findSessionFile(root, args[1])
		if err != nil {
			return err
		}
		return showSession(path)
	case "fork":
		if len(args) < 2 {
			return fmt.Errorf("session fork needs an id")
		}
		path, err := findSessionFile(root, args[1])
		if err != nil {
			return err
		}
		mgr := session.NewManager(root)
		sess, _, err := mgr.Load(path)
		if err != nil {
			return err
		}
		child, err := sess.Fork()
		if err != nil {
			return err
		}
		fmt.Printf("forked to %s\n", child.Path())
		return child.Close()
	case "resume":
		if len(args) < 2 {
			return fmt.Errorf("session resume needs an id")
		}
		fs := flag.NewFlagSet("resume", flag.ContinueOnError)
		promptFlag := fs.String("p", "", "prompt to continue the session with")
		providerFlag := fs.String("provider", "", "provider id override")
		modelFlag := fs.String("model", "", "model id override")
		profileFlag := fs.String("profile", "", "agent profile override")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *promptFlag == "" {
			return fmt.Errorf("session resume needs -p <prompt>")
		}
		return resumeSession(root, args[1], *providerFlag, *modelFlag, *profileFlag, *promptFlag)
	default:
		return fmt.Errorf("unknown session command %q", args[0])
	}
}

func listSessions(root string) error {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(files) == 0 {
		fmt.Println("no sessions under", root)
		return nil
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	for _, path := range files {
		header, entries, _, err := session.Inspect(path)
		if err != nil {
			fmt.Printf("%s  <unreadable: %v>\n", filepath.Base(path), err)
			continue
		}
		snippet := firstUserSnippet(entries, 60)
		marker := ""
		if header.ParentSession != "" {
			marker = " [forked]"
		}
		fmt.Printf("%s  %s%s  %s\n", header.Timestamp, shortID(header.ID), marker, snippet)
	}
	return nil
}

func showSession(path string) error {
	header, entries, onChain, err := session.Inspect(path)
	if err != nil {
		return err
	}
	fmt.Printf("session %s  %s  cwd=%s\n", header.ID, header.Timestamp, header.Cwd)
	if header.ParentSession != "" {
		fmt.Printf("  forked from %s\n", header.ParentSession)
	}
	for _, e := range entries {
		mark := " "
		if onChain[e.ID] {
			mark = "*"
		}
		fmt.Printf("%s %s %-22s %s\n", mark, e.ID, e.Type, describeEntry(e))
	}
	return nil
}

func describeEntry(e session.Entry) string {
	switch e.Type {
	case session.TypeMessage:
		if e.Message == nil {
			return "<missing message>"
		}
		return fmt.Sprintf("%s: %s", e.Message.Role, snippet(entryText(*e.Message), 70))
	case session.TypeModelChange:
		return fmt.Sprintf("%s/%s", e.Provider, e.ModelID)
	case session.TypeThinkingLevelChange:
		return string(e.ThinkingLevel)
	case session.TypeCompaction:
		return fmt.Sprintf("summarized %d messages (%d tokens before): %s", e.SummarizedCount, e.TokensBefore, snippet(e.Summary, 60))
	default:
		return ""
	}
}

func entryText(m llm.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if t, ok := b.(llm.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

func firstUserSnippet(entries []session.Entry, limit int) string {
	for _, e := range entries {
		if e.Type == session.TypeMessage && e.Message != nil && e.Message.Role == llm.RoleUser {
			return snippet(strings.TrimSpace(entryText(*e.Message)), limit)
		}
	}
	return ""
}

func resumeSession(root, id, providerFlag, modelFlag, profileFlag, prompt string) error {
	path, err := findSessionFile(root, id)
	if err != nil {
		return err
	}
	mgr := session.NewManager(root)
	sess, state, err := mgr.Load(path)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	// a recorded model_change overrides flags-that-were-not-given
	if state.Model != nil {
		providerFlag = firstNonEmpty(providerFlag, state.Model.ProviderID)
		modelFlag = firstNonEmpty(modelFlag, state.Model.ModelID)
	}
	app, err := prepare(providerFlag, modelFlag, profileFlag)
	if err != nil {
		return err
	}
	options, _, err := app.buildOptions("", state.Messages, sess)
	if err != nil {
		return err
	}
	a := agent.New(app.provider, app.model, options...)
	rec := session.NewRecorder(sess)
	a.Subscribe(rec.Handle)
	a.Subscribe(renderEvent)
	if err := a.Prompt(context.Background(), prompt); err != nil {
		return err
	}
	if err := rec.Err(); err != nil {
		return fmt.Errorf("session recording: %w", err)
	}
	return nil
}

// findSessionFile resolves an id prefix (of the session id or file name)
// to exactly one session file.
func findSessionFile(root, idOrPrefix string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			base := strings.TrimSuffix(d.Name(), ".jsonl")
			if strings.Contains(base, idOrPrefix) {
				matches = append(matches, path)
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no session matching %q under %s", idOrPrefix, root)
	default:
		return "", fmt.Errorf("%q matches %d sessions; be more specific", idOrPrefix, len(matches))
	}
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func snippet(s string, limit int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
