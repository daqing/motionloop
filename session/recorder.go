package session

import (
	"sync"

	"github.com/daqing/motionloop/agent"
)

// Recorder persists finalized agent messages to a session file — the
// write-ahead half of the persistence loop. Attach it with Subscribe; read
// back with Manager.Load.
type Recorder struct {
	mu   sync.Mutex
	sess *Session
	err  error
}

// NewRecorder wires a session to agent message lifecycle events.
func NewRecorder(sess *Session) *Recorder {
	return &Recorder{sess: sess}
}

// Handle implements the agent event subscription contract; only finalized
// messages are recorded.
func (r *Recorder) Handle(ev agent.Event) {
	e, ok := ev.(agent.MessageEnd)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return
	}
	r.err = r.sess.AppendMessage(e.Message)
}

// Err reports the first write failure, if any; the recorder stops writing
// after a failure to avoid gaps.
func (r *Recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}
