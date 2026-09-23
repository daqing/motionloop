// Package session persists agent transcripts as append-only JSONL files
// using a pi-compatible entry schema: a leading session header, then
// entries linked into a tree by id/parentId. Replaying the current leaf
// chain rebuilds the conversation, prompt sections, and tool loadout —
// there is no separate state file. The schema is structurally aligned with
// pi; byte-level compatibility is not promised.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/daqing/motionloop/llm"
)

// CurrentVersion is the entry-schema version this build writes and reads.
// Loading older versions migrates forward in migrate; newer versions fail.
const CurrentVersion = 1

// Entry type values. Motionloop-specific events extend this set without
// touching the envelope.
const (
	TypeMessage             = "message"
	TypeModelChange         = "model_change"
	TypeThinkingLevelChange = "thinking_level_change"
	TypeUsage               = "usage"
	TypeCompaction          = "compaction"
)

// Header is the first line of a session file. It is metadata only and not
// part of the entry tree.
type Header struct {
	Type          string `json:"type"` // "session"
	Version       int    `json:"version"`
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Cwd           string `json:"cwd"`
	ParentSession string `json:"parentSession,omitempty"`
}

// Entry is one line after the header. The envelope is closed — type, id,
// parentId, timestamp — and payload fields are selected by Type.
type Entry struct {
	Type          string            `json:"type"`
	ID            string            `json:"id"`
	ParentID      string            `json:"parentId,omitempty"`
	Timestamp     string            `json:"timestamp"`
	Message       *llm.Message      `json:"message,omitempty"`
	Provider      string            `json:"provider,omitempty"`
	ModelID       string            `json:"modelId,omitempty"`
	ThinkingLevel llm.ThinkingLevel `json:"thinkingLevel,omitempty"`
	Usage         *llm.Usage        `json:"usage,omitempty"`
	// compaction payload
	Summary         string `json:"summary,omitempty"`
	TokensBefore    int64  `json:"tokensBefore,omitempty"`
	SummarizedCount int    `json:"summarizedCount,omitempty"`
}

// newEntryID returns a short unique entry id (8 hex chars, pi-style).
func newEntryID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("session: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// newSessionID returns a UUIDv4-shaped session id.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("session: crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
