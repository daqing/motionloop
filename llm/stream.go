package llm

// StopReason is the terminal state of one assistant response. The transient
// "pending" state found in streaming transcripts is never represented here:
// a message is only finalized with a terminal reason.
type StopReason string

// Terminal completion reasons, aligned with pi's assistant messages.
const (
	StopReasonStop    StopReason = "stop"
	StopReasonLength  StopReason = "length"
	StopReasonToolUse StopReason = "toolUse"
	StopReasonError   StopReason = "error"
	StopReasonAborted StopReason = "aborted"
)

// StreamEvent is emitted by providers while streaming one assistant
// response. Every well-formed stream ends with exactly one Stop event.
// Provider and runtime failures are encoded as Stop{Reason:
// StopReasonError} rather than surfaced as errors, matching the Stream
// contract the agent loop relies on.
type StreamEvent interface {
	isStreamEvent()
}

var _ []StreamEvent = []StreamEvent{TextDelta{}, ThinkingDelta{}, ToolCallDelta{}, UsageUpdate{}, Stop{}}

// TextDelta appends to the assistant's text content.
type TextDelta struct {
	Delta string
}

// ThinkingDelta appends to the assistant's thinking content.
type ThinkingDelta struct {
	Delta string
}

// ToolCallDelta carries one fragment of a tool call. Fragments sharing an
// Index concatenate: ID and Name arrive on the first fragment, and argument
// fragments append to ArgumentsDelta.
type ToolCallDelta struct {
	Index          int
	ID             string
	Name           string
	ArgumentsDelta string
}

// UsageUpdate reports token usage accumulated so far.
type UsageUpdate struct {
	Usage Usage
}

// Stop terminates one assistant response.
type Stop struct {
	Reason       StopReason
	ErrorMessage string
}

func (TextDelta) isStreamEvent()     {}
func (ThinkingDelta) isStreamEvent() {}
func (ToolCallDelta) isStreamEvent() {}
func (UsageUpdate) isStreamEvent()   {}
func (Stop) isStreamEvent()          {}
