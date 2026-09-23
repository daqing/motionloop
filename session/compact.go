package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/daqing/motionloop/llm"
)

// Compaction tuning defaults.
const (
	// DefaultCompactionThreshold is the estimated-token trigger used when
	// settings leave it unset.
	DefaultCompactionThreshold = 24000
	// DefaultKeepRecent is how many trailing non-system messages compaction
	// keeps verbatim.
	DefaultKeepRecent = 6
)

// TokenEstimator approximates token counts from the character mix: ASCII
// text averages ASCIIPerToken chars per token, non-ASCII (CJK-heavy) text
// CJKPerToken runes per token — both adjustable per language mix.
type TokenEstimator struct {
	ASCIIPerToken float64
	CJKPerToken   float64
}

// DefaultEstimator returns the stock estimator (4 chars/token ASCII,
// 1.5 runes/token CJK).
func DefaultEstimator() TokenEstimator {
	return TokenEstimator{ASCIIPerToken: 4, CJKPerToken: 1.5}
}

// Estimate sums the approximate tokens of the messages' text content.
func (e TokenEstimator) Estimate(msgs []llm.Message) int {
	ascii, other := 0, 0
	for _, m := range msgs {
		for _, b := range m.Content {
			switch blk := b.(type) {
			case llm.TextBlock:
				ascii, other = countMix(blk.Text, ascii, other)
			case llm.ThinkingBlock:
				ascii, other = countMix(blk.Thinking, ascii, other)
			case llm.ToolCallBlock:
				ascii, other = countMix(string(blk.Arguments), ascii, other)
			}
		}
	}
	return int(float64(ascii)/e.asciiPerToken() + float64(other)/e.cjkPerToken())
}

func (e TokenEstimator) asciiPerToken() float64 {
	if e.ASCIIPerToken <= 0 {
		return 4
	}
	return e.ASCIIPerToken
}

func (e TokenEstimator) cjkPerToken() float64 {
	if e.CJKPerToken <= 0 {
		return 1.5
	}
	return e.CJKPerToken
}

func countMix(s string, ascii, other int) (int, int) {
	for _, r := range s {
		if r < 128 {
			ascii++
		} else {
			other++
		}
	}
	return ascii, other
}

// compactedView drops the oldest summarizedCount non-system messages and
// splices the summary in their place. Live transforms and replay folding
// share this function, which keeps both views identical.
func compactedView(messages []llm.Message, summary string, summarizedCount int) []llm.Message {
	if summarizedCount <= 0 {
		return messages
	}
	out := make([]llm.Message, 0, len(messages))
	dropped := 0
	inserted := false
	insert := func(ts int64) {
		out = append(out, llm.Message{
			Role:      llm.RoleSystem,
			Content:   []llm.ContentBlock{llm.TextBlock{Text: "# Compacted conversation summary\n\n" + summary}},
			Timestamp: ts,
		})
		inserted = true
	}
	for _, m := range messages {
		if m.Role == llm.RoleSystem {
			out = append(out, m)
			continue
		}
		if dropped < summarizedCount {
			dropped++
			continue
		}
		if !inserted {
			insert(m.Timestamp)
		}
		out = append(out, m)
	}
	if !inserted && summary != "" {
		insert(time.Now().UnixMilli()) // no kept messages; the summary closes the view
	}
	return out
}

// Compactor compacts the request-side transcript view once it grows past
// a threshold: aged messages are summarized through the provider, a
// compaction entry is appended so replay yields the same view, and the
// original session rows are never removed. Attach with
// agent.WithTransformContext(compactor.Transform).
type Compactor struct {
	Sess      *Session
	Provider  llm.Provider
	Model     llm.Model
	Opts      llm.StreamOptions
	Estimator TokenEstimator
	// Threshold is the estimated-token trigger; 0 → DefaultCompactionThreshold.
	Threshold int
	// KeepRecent is the verbatim tail; 0 → DefaultKeepRecent.
	KeepRecent int
	// SummaryModel overrides the summarizing model; zero value uses Model.
	SummaryModel llm.Model

	mu              sync.Mutex
	summarizedCount int
	summary         string
}

// Transform implements the agent.TransformContext hook.
func (c *Compactor) Transform(ctx context.Context, messages []llm.Message) []llm.Message {
	return c.transform(ctx, messages, false)
}

// Force compacts regardless of the threshold — the manual /compact path.
func (c *Compactor) Force(ctx context.Context, messages []llm.Message) []llm.Message {
	return c.transform(ctx, messages, true)
}

func (c *Compactor) transform(ctx context.Context, messages []llm.Message, force bool) []llm.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Threshold < 0 && !force {
		return messages
	}
	view := compactedView(messages, c.summary, c.summarizedCount)
	if !force && c.estimator().Estimate(view) <= c.threshold() {
		return view
	}
	span := c.agedSpan(messages)
	if len(span) == 0 {
		return view
	}
	summary, err := c.summarize(ctx, span)
	if err != nil {
		return view // degrading to the uncompacted view beats failing the run
	}
	count := c.summarizedCount + len(span)
	if err := c.Sess.AppendCompaction(summary, int64(c.estimator().Estimate(messages)), count); err == nil {
		c.summary, c.summarizedCount = summary, count
	}
	return compactedView(messages, c.summary, c.summarizedCount)
}

// agedSpan selects the non-system messages between what earlier
// compactions already summarized and the verbatim tail.
func (c *Compactor) agedSpan(messages []llm.Message) []llm.Message {
	var nonSystem []llm.Message
	for _, m := range messages {
		if m.Role != llm.RoleSystem {
			nonSystem = append(nonSystem, m)
		}
	}
	tailStart := len(nonSystem) - c.keepRecent()
	if tailStart <= c.summarizedCount {
		return nil
	}
	return nonSystem[c.summarizedCount:tailStart]
}

func (c *Compactor) summarize(ctx context.Context, span []llm.Message) (string, error) {
	model := c.SummaryModel
	if model.ModelID == "" {
		model = c.Model
	}
	msgs := append([]llm.Message{{
		Role:    llm.RoleUser,
		Content: []llm.ContentBlock{llm.TextBlock{Text: "Summarize the conversation so far for an agent continuing this work. Preserve: task goals and the current objective, key decisions and their reasons, open items and next steps, and important file paths and commands. Be concise and factual; output only the summary."}},
	}}, span...)

	ch, err := c.Provider.Stream(ctx, model, msgs, llm.StreamOptions{APIKey: c.Opts.APIKey, BaseURL: c.Opts.BaseURL})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for ev := range ch {
		switch e := ev.(type) {
		case llm.TextDelta:
			sb.WriteString(e.Delta)
		case llm.Stop:
			if e.Reason == llm.StopReasonError {
				return "", fmt.Errorf("session: summarize failed: %s", e.ErrorMessage)
			}
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		return "", fmt.Errorf("session: summarize produced no text")
	}
	return sb.String(), nil
}

func (c *Compactor) threshold() int {
	if c.Threshold == 0 {
		return DefaultCompactionThreshold
	}
	return c.Threshold
}

func (c *Compactor) keepRecent() int {
	if c.KeepRecent == 0 {
		return DefaultKeepRecent
	}
	return c.KeepRecent
}

func (c *Compactor) estimator() TokenEstimator {
	if e := c.Estimator; e != (TokenEstimator{}) {
		return e
	}
	return DefaultEstimator()
}
