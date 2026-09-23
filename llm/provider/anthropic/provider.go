// Package anthropic implements a Provider for the Anthropic Messages API:
// system prompts as a top-level field, tool_use/tool_result blocks,
// extended thinking, and prompt caching. Error semantics match
// openaicompat: failures arrive as Stop events, never as returned errors.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/daqing/motionloop/llm"
)

const (
	apiVersionHeader = "2023-06-01"
	defaultMaxTokens = 8192
)

// Provider serves one Anthropic-compatible endpoint identity.
type Provider struct {
	providerID string
	baseURL    string
	models     []llm.Model
}

// New binds a provider identity to an endpoint base URL and model list.
func New(providerID, baseURL string, models []llm.Model) *Provider {
	return &Provider{
		providerID: providerID,
		baseURL:    strings.TrimRight(baseURL, "/"),
		models:     models,
	}
}

// ID implements llm.Provider.
func (p *Provider) ID() string { return p.providerID }

// Models returns the endpoint's built-in catalog entries.
func (p *Provider) Models() []llm.Model { return p.models }

// Capabilities implements llm.Provider.
func (p *Provider) Capabilities(model llm.Model) llm.Capabilities {
	if model.Capabilities != (llm.Capabilities{}) {
		return model.Capabilities
	}
	return llm.Capabilities{Images: true, Cache: true, ParallelToolCalls: true}
}

// Stream implements llm.Provider.
func (p *Provider) Stream(ctx context.Context, model llm.Model, messages []llm.Message, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body, err := p.buildBody(model, messages, opts)
	if err != nil {
		return failed(err)
	}
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = p.baseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return failed(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("anthropic-version", apiVersionHeader)
	if opts.APIKey != "" {
		req.Header.Set("x-api-key", opts.APIKey)
	}

	ch := make(chan llm.StreamEvent, 16)
	go func() {
		defer close(ch)
		client := &http.Client{}
		if opts.Timeout > 0 {
			client.Timeout = opts.Timeout
		}
		resp, err := client.Do(req)
		if err != nil {
			send(ctx, ch, p.errStop(ctx, "request failed: %v", err))
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			send(ctx, ch, p.httpError(resp))
			return
		}
		p.streamBody(ctx, resp.Body, ch)
	}()
	return ch, nil
}

func (p *Provider) buildBody(model llm.Model, messages []llm.Message, opts llm.StreamOptions) ([]byte, error) {
	system, msgs, err := convertMessages(messages)
	if err != nil {
		return nil, err
	}
	if len(system) > 0 && model.Capabilities.Cache {
		system[len(system)-1].CacheControl = &antCacheControl{Type: "ephemeral"}
	}

	maxTokens := opts.MaxTokens
	budget := thinkingBudget(opts.ThinkingLevel)
	if budget > 0 && !model.Capabilities.Thinking {
		budget = 0
	}
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
		if model.MaxOutput > 0 && model.MaxOutput < maxTokens {
			maxTokens = model.MaxOutput
		}
	}
	if budget > 0 && maxTokens <= budget {
		maxTokens = budget + 4096
	}

	req := antRequest{
		Model:     model.ModelID,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
		Stream:    true,
	}
	if opts.Temperature != 0 {
		t := opts.Temperature
		req.Temperature = &t
	}
	if budget > 0 {
		req.Thinking = &antThinking{Type: "enabled", BudgetTokens: budget}
	}
	if len(opts.Tools) > 0 {
		for _, td := range opts.Tools {
			req.Tools = append(req.Tools, antTool{
				Name:        td.Name,
				Description: td.Description,
				InputSchema: td.Parameters,
			})
		}
	}
	return json.Marshal(req)
}

func (p *Provider) streamBody(ctx context.Context, body io.Reader, ch chan<- llm.StreamEvent) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	usage := llm.Usage{}
	finish := ""
	sawStop := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") ||
			!strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message *struct {
				Usage *antUsage `json:"usage"`
			} `json:"message"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			UsageOut *antUsage `json:"usage"`
			Error    *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			send(ctx, ch, p.errStop(ctx, "malformed event: %v", err))
			return
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				ev.Message.Usage.apply(&usage)
			}
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				send(ctx, ch, llm.ToolCallDelta{Index: ev.Index, ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name})
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				send(ctx, ch, llm.TextDelta{Delta: ev.Delta.Text})
			case "thinking_delta":
				send(ctx, ch, llm.ThinkingDelta{Delta: ev.Delta.Thinking})
			case "input_json_delta":
				send(ctx, ch, llm.ToolCallDelta{Index: ev.Index, ArgumentsDelta: ev.Delta.PartialJSON})
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				finish = ev.Delta.StopReason
			}
			if ev.UsageOut != nil {
				ev.UsageOut.apply(&usage)
				send(ctx, ch, llm.UsageUpdate{Usage: usage})
			}
		case "message_stop":
			sawStop = true
		case "error":
			msg := "unknown error"
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			send(ctx, ch, p.errStop(ctx, "%s", msg))
			return
		}
	}
	if err := scanner.Err(); err != nil {
		send(ctx, ch, p.errStop(ctx, "stream interrupted: %v", err))
		return
	}
	if !sawStop {
		send(ctx, ch, p.errStop(ctx, "stream interrupted: ended without message_stop"))
		return
	}
	send(ctx, ch, llm.Stop{Reason: stopReasonOf(finish)})
}

func (p *Provider) httpError(resp *http.Response) llm.Stop {
	snippet := make([]byte, 2048)
	n, _ := io.ReadFull(resp.Body, snippet)
	msg := strings.TrimSpace(string(snippet[:n]))
	var wrapped struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(snippet[:n], &wrapped) == nil && wrapped.Error.Message != "" {
		msg = wrapped.Error.Message
	}
	return llm.Stop{
		Reason:       llm.StopReasonError,
		ErrorMessage: fmt.Sprintf("%s: HTTP %d: %s", p.providerID, resp.StatusCode, msg),
	}
}

func (p *Provider) errStop(ctx context.Context, format string, args ...any) llm.Stop {
	msg := fmt.Sprintf(format, args...)
	if ctx.Err() != nil {
		return llm.Stop{Reason: llm.StopReasonAborted, ErrorMessage: msg}
	}
	return llm.Stop{Reason: llm.StopReasonError, ErrorMessage: p.providerID + ": " + msg}
}

func send(ctx context.Context, ch chan<- llm.StreamEvent, ev llm.StreamEvent) {
	select {
	case ch <- ev:
		return
	default:
	}
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

func failed(err error) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 1)
	ch <- llm.Stop{Reason: llm.StopReasonError, ErrorMessage: err.Error()}
	close(ch)
	return ch, nil
}

func thinkingBudget(level llm.ThinkingLevel) int64 {
	switch level {
	case llm.ThinkingMinimal:
		return 1024
	case llm.ThinkingLow:
		return 2048
	case llm.ThinkingMedium:
		return 8192
	case llm.ThinkingHigh:
		return 16384
	default:
		return 0
	}
}

func stopReasonOf(finish string) llm.StopReason {
	switch finish {
	case "max_tokens":
		return llm.StopReasonLength
	case "tool_use":
		return llm.StopReasonToolUse
	default:
		return llm.StopReasonStop
	}
}
