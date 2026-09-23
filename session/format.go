package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/daqing/motionloop/llm"
)

// State is the replayed session state: a pure function of the file. It is
// the resume, fork, and (later) compaction baseline.
type State struct {
	Messages      []llm.Message
	Sections      map[string]string
	Tools         []string
	Model         *llm.Model
	ThinkingLevel llm.ThinkingLevel
	Usage         *llm.Usage
	TipID         string
	TornTrailing  bool
}

type parsedFile struct {
	header  Header
	entries []Entry
	torn    bool
}

// parseFile decodes one session file. A torn trailing line — the signature
// of a crash mid-append — is dropped and reported; unparseable lines
// anywhere else are hard corruption.
func parseFile(r io.Reader) (parsedFile, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return parsedFile{}, fmt.Errorf("session: read: %w", err)
	}
	var out parsedFile
	sawHeader := false
	for _, raw := range bytes.Split(data, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		if !sawHeader {
			if err := json.Unmarshal(line, &out.header); err != nil {
				return out, fmt.Errorf("session: header line: %w", err)
			}
			if out.header.Type != "session" {
				return out, fmt.Errorf("session: first line type = %q, want \"session\"", out.header.Type)
			}
			sawHeader = true
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			if isLastNonEmptyLine(data, raw) {
				out.torn = true
				return out, nil
			}
			return out, fmt.Errorf("session: corrupt entry line: %w", err)
		}
		out.entries = append(out.entries, e)
	}
	if !sawHeader {
		return out, fmt.Errorf("session: file has no header line")
	}
	return out, nil
}

func isLastNonEmptyLine(data, line []byte) bool {
	idx := bytes.LastIndex(data, line)
	if idx < 0 {
		return false
	}
	return len(bytes.TrimSpace(data[idx+len(line):])) == 0
}

// migrate upgrades entries from older schema versions. Version 1 is the
// first released schema, so no forward migrations exist yet; the hook is
// the sanctioned place to add them.
func migrate(h Header, entries []Entry) ([]Entry, error) {
	switch {
	case h.Version == CurrentVersion:
		return entries, nil
	case h.Version < CurrentVersion:
		return nil, fmt.Errorf("session: no migration path from version %d to %d", h.Version, CurrentVersion)
	default:
		return nil, fmt.Errorf("session: file version %d is newer than supported version %d", h.Version, CurrentVersion)
	}
}

// chainOf walks the entry tree from one entry (the file's last entry when
// fromID is empty) back to its root, returning the chain leaf-first. When
// a parent reference cannot be resolved inside this file, crossRef carries
// that id: it names the fork point inside the parent session file.
func chainOf(entries []Entry, fromID string) (chain []Entry, crossRef string, err error) {
	if len(entries) == 0 {
		return nil, "", nil
	}
	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if _, dup := byID[e.ID]; dup {
			return nil, "", fmt.Errorf("session: duplicate entry id %s", e.ID)
		}
		byID[e.ID] = e
	}
	var cur Entry
	if fromID == "" {
		cur = entries[len(entries)-1]
	} else {
		e, ok := byID[fromID]
		if !ok {
			return nil, "", fmt.Errorf("session: entry %s not found", fromID)
		}
		cur = e
	}
	seen := map[string]bool{}
	for {
		if seen[cur.ID] {
			return nil, "", fmt.Errorf("session: cycle at entry %s", cur.ID)
		}
		seen[cur.ID] = true
		chain = append(chain, cur)
		if cur.ParentID == "" {
			return chain, "", nil
		}
		next, ok := byID[cur.ParentID]
		if !ok {
			return chain, cur.ParentID, nil
		}
		cur = next
	}
}

// foldState applies a leaf-first chain onto a base state, root entries
// first.
func foldState(base State, chain []Entry) State {
	st := base
	for i := len(chain) - 1; i >= 0; i-- {
		applyEntry(&st, chain[i])
	}
	return st
}

func applyEntry(st *State, e Entry) {
	switch e.Type {
	case TypeMessage:
		if e.Message == nil {
			return
		}
		st.Messages = append(st.Messages, *e.Message)
		if e.Message.Role == llm.RoleSystem {
			applySystemPatch(st, *e.Message)
		}
	case TypeModelChange:
		st.Model = &llm.Model{ProviderID: e.Provider, ModelID: e.ModelID}
	case TypeThinkingLevelChange:
		st.ThinkingLevel = e.ThinkingLevel
	case TypeUsage:
		if e.Usage != nil {
			u := *e.Usage
			st.Usage = &u
		}
	case TypeCompaction:
		st.Messages = compactedView(st.Messages, e.Summary, e.SummarizedCount)
	}
}

// applySystemPatch folds prompt sections and tool loadout changes from one
// system message. An empty section value removes the section (the
// map[string]string representation of pi's null).
func applySystemPatch(st *State, m llm.Message) {
	for k, v := range m.Sections {
		if v == "" {
			delete(st.Sections, k)
		} else {
			if st.Sections == nil {
				st.Sections = map[string]string{}
			}
			st.Sections[k] = v
		}
	}
	for _, t := range m.ToolsAdded {
		if !containsString(st.Tools, t.Name) {
			st.Tools = append(st.Tools, t.Name)
		}
	}
	for _, r := range m.ToolsRemoved {
		st.Tools = removeString(st.Tools, r)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func removeString(list []string, s string) []string {
	out := list[:0]
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// sortedSectionKeys is a test helper giving deterministic section order.
func sortedSectionKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
