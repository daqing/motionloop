package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daqing/motionloop/llm"
)

// Manager creates and loads session files under a root directory, laid out
// as <root>/<workspace-slug>/<timestamp>_<id>.jsonl.
type Manager struct {
	Root string
}

// NewManager uses an explicit sessions root.
func NewManager(root string) *Manager { return &Manager{Root: root} }

// DefaultRoot is ~/.motionloop/sessions.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("session: home directory: %w", err)
	}
	return filepath.Join(home, ".motionloop", "sessions"), nil
}

// WorkspaceSlug flattens a cwd into one path segment (pi-style: separators
// become dashes).
func WorkspaceSlug(cwd string) string {
	s := strings.TrimPrefix(filepath.ToSlash(cwd), "/")
	s = strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(s)
	if s == "" {
		s = "default"
	}
	return s
}

// Create starts a new session file for one workspace.
func (m *Manager) Create(cwd string) (*Session, error) {
	id := newSessionID()
	dir := filepath.Join(m.Root, WorkspaceSlug(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("session: create workspace dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s_%s.jsonl", time.Now().UTC().Format("20060102-150405"), id))
	h := Header{Type: "session", Version: CurrentVersion, ID: id, Timestamp: nowISO(), Cwd: cwd}
	s := &Session{path: path, header: h, ids: map[string]bool{}}
	if err := s.writeHeader(); err != nil {
		return nil, err
	}
	return s, nil
}

// Load replays a session file and reopens it for appending — the resume
// path. The returned State is a pure function of the file (plus parent
// files, when the session was forked).
func (m *Manager) Load(path string) (*Session, State, error) {
	pf, err := parsePath(path)
	if err != nil {
		return nil, State{}, err
	}
	entries, err := migrate(pf.header, pf.entries)
	if err != nil {
		return nil, State{}, err
	}

	s := &Session{path: path, header: pf.header, ids: map[string]bool{}}
	for _, e := range entries {
		s.ids[e.ID] = true
	}
	if len(entries) > 0 {
		s.tipID = entries[len(entries)-1].ID
	}

	ownChain, crossRef, err := chainOf(entries, "")
	if err != nil {
		return nil, State{}, fmt.Errorf("session: %s: %w", path, err)
	}
	if crossRef != "" && pf.header.ParentSession == "" {
		return nil, State{}, fmt.Errorf("session: %s: entry references missing parent %s", path, crossRef)
	}

	base := State{}
	if pf.header.ParentSession != "" {
		base, err = m.loadUpTo(pf.header.ParentSession, crossRef)
		if err != nil {
			return nil, State{}, err
		}
	}
	state := foldState(base, ownChain)
	state.TornTrailing = pf.torn
	switch {
	case len(ownChain) > 0:
		state.TipID = ownChain[0].ID
	case base.TipID != "":
		state.TipID = base.TipID
		s.tipID = base.TipID
	}
	if err := s.openForAppend(); err != nil {
		return nil, State{}, err
	}
	return s, state, nil
}

// loadUpTo replays one file's chain up to fromID (its whole current chain
// when fromID is empty), recursing into parent files at fork points.
func (m *Manager) loadUpTo(path, fromID string) (State, error) {
	pf, err := parsePath(path)
	if err != nil {
		return State{}, err
	}
	entries, err := migrate(pf.header, pf.entries)
	if err != nil {
		return State{}, err
	}
	chain, crossRef, err := chainOf(entries, fromID)
	if err != nil {
		return State{}, fmt.Errorf("session: %s: %w", path, err)
	}
	var base State
	if crossRef != "" {
		if pf.header.ParentSession == "" {
			return State{}, fmt.Errorf("session: %s: entry references missing parent %s", path, crossRef)
		}
		base, err = m.loadUpTo(pf.header.ParentSession, crossRef)
		if err != nil {
			return State{}, err
		}
	}
	st := foldState(base, chain)
	if fromID == "" && len(chain) > 0 {
		st.TipID = chain[0].ID
	}
	if fromID != "" {
		st.TipID = fromID
	}
	return st, nil
}

func parsePath(path string) (parsedFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return parsedFile{}, fmt.Errorf("session: open: %w", err)
	}
	defer func() { _ = f.Close() }()
	pf, err := parseFile(f)
	if err != nil {
		return parsedFile{}, fmt.Errorf("session: %s: %w", path, err)
	}
	return pf, nil
}

// Inspect parses a session file for display: the header, every entry in
// file order, and the set of entry ids on the current chain.
func Inspect(path string) (Header, []Entry, map[string]bool, error) {
	pf, err := parsePath(path)
	if err != nil {
		return Header{}, nil, nil, err
	}
	entries, err := migrate(pf.header, pf.entries)
	if err != nil {
		return Header{}, nil, nil, err
	}
	chain, _, err := chainOf(entries, "")
	if err != nil {
		return Header{}, nil, nil, fmt.Errorf("session: %s: %w", path, err)
	}
	onChain := map[string]bool{}
	for _, e := range chain {
		onChain[e.ID] = true
	}
	return pf.header, entries, onChain, nil
}

// Session is an open append-only session file.
type Session struct {
	path   string
	header Header
	tipID  string
	ids    map[string]bool

	mu   sync.Mutex
	file *os.File
}

// Path is the file location.
func (s *Session) Path() string { return s.path }

// Header is the file's session header.
func (s *Session) Header() Header { return s.header }

// TipID is the current branch point; appends parent here.
func (s *Session) TipID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tipID
}

// AppendMessage records one finalized message.
func (s *Session) AppendMessage(msg llm.Message) error {
	return s.append(Entry{Type: TypeMessage, Message: &msg})
}

// AppendModelChange records a mid-session model switch.
func (s *Session) AppendModelChange(providerID, modelID string) error {
	return s.append(Entry{Type: TypeModelChange, Provider: providerID, ModelID: modelID})
}

// AppendThinkingLevelChange records a thinking-level switch.
func (s *Session) AppendThinkingLevelChange(level llm.ThinkingLevel) error {
	return s.append(Entry{Type: TypeThinkingLevelChange, ThinkingLevel: level})
}

// AppendUsage records a usage summary.
func (s *Session) AppendUsage(u llm.Usage) error {
	return s.append(Entry{Type: TypeUsage, Usage: &u})
}

// AppendCompaction records one compaction: summary text, the estimated
// token count before compacting, and how many non-system messages of the
// current fold state were summarized.
func (s *Session) AppendCompaction(summary string, tokensBefore int64, summarizedCount int) error {
	return s.append(Entry{
		Type:            TypeCompaction,
		Summary:         summary,
		TokensBefore:    tokensBefore,
		SummarizedCount: summarizedCount,
	})
}

// Branch rewinds the append point to an earlier entry in this file; the
// next append starts a sibling chain (in-place branching). The rewind
// becomes durable when the first new entry lands.
func (s *Session) Branch(fromEntryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ids[fromEntryID] {
		return fmt.Errorf("session: unknown entry %q", fromEntryID)
	}
	s.tipID = fromEntryID
	return nil
}

// Fork opens a new session file whose chain continues from the current
// tip; the child header records this file via parentSession. Parent and
// child evolve independently afterwards.
func (s *Session) Fork() (*Session, error) {
	s.mu.Lock()
	parent := s.header
	tip := s.tipID
	s.mu.Unlock()

	id := newSessionID()
	path := filepath.Join(filepath.Dir(s.path), fmt.Sprintf("%s_%s.jsonl", time.Now().UTC().Format("20060102-150405"), id))
	h := Header{
		Type:          "session",
		Version:       CurrentVersion,
		ID:            id,
		Timestamp:     nowISO(),
		Cwd:           parent.Cwd,
		ParentSession: s.path,
	}
	child := &Session{path: path, header: h, tipID: tip, ids: map[string]bool{}}
	if err := child.writeHeader(); err != nil {
		return nil, err
	}
	return child, nil
}

// Close releases the file handle.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

func (s *Session) append(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		if err := s.openForAppend(); err != nil {
			return err
		}
	}
	e.ID = newEntryID()
	e.ParentID = s.tipID
	e.Timestamp = nowISO()
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("session: encode entry: %w", err)
	}
	if _, err := s.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("session: append entry: %w", err)
	}
	s.tipID = e.ID
	s.ids[e.ID] = true
	return nil
}

func (s *Session) openForAppend() error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("session: open for append: %w", err)
	}
	s.file = f
	return nil
}

func (s *Session) writeHeader() error {
	line, err := json.Marshal(s.header)
	if err != nil {
		return fmt.Errorf("session: encode header: %w", err)
	}
	if err := s.openForAppend(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("session: write header: %w", err)
	}
	return nil
}
