// Package openaicompat implements a Provider for any OpenAI-compatible
// chat completions endpoint: OpenAI, GLM, DeepSeek, Kimi, Ollama, vLLM, and
// custom gateways. One code path serves many vendor identities via New.
package openaicompat

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

// Provider serves one OpenAI-compatible endpoint identity.
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

// Capabilities implements llm.Provider. Models without declared
// capabilities get conservative defaults.
func (p *Provider) Capabilities(model llm.Model) llm.Capabilities {
	if model.Capabilities != (llm.Capabilities{}) {
		return model.Capabilities
	}
	return llm.Capabilities{Images: true, ParallelToolCalls: true}
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
	baseURL := opts.Credentials.BaseURL
	if baseURL == "" {
		baseURL = p.baseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return failed(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if opts.Credentials.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+opts.Credentials.APIKey)
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
	msgs, err := chatMessages(messages)
	if err != nil {
		return nil, err
	}
	req := chatRequest{
		Model:         model.ModelID,
		Messages:      msgs,
		Stream:        true,
		StreamOptions: &chatStreamOptions{IncludeUsage: true},
	}
	if len(opts.Tools) > 0 {
		req.Tools = chatTools(opts.Tools)
	}
	if opts.MaxTokens > 0 {
		req.MaxTokens = opts.MaxTokens
	}
	if opts.Temperature != 0 {
		req.Temperature = opts.Temperature
	}
	if e := reasoningEffort(opts.ThinkingLevel); e != "" {
		req.ReasoningEffort = e
	}
	return json.Marshal(req)
}

func (p *Provider) streamBody(ctx context.Context, body io.Reader, ch chan<- llm.StreamEvent) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	finish := ""
	sawDone := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			sawDone = true
			break
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			send(ctx, ch, p.errStop(ctx, "malformed chunk: %v", err))
			return
		}
		for _, ev := range chunk.events() {
			send(ctx, ch, ev)
		}
		if fr := chunk.finishReason(); fr != "" {
			finish = fr
		}
	}
	if err := scanner.Err(); err != nil {
		send(ctx, ch, p.errStop(ctx, "stream interrupted: %v", err))
		return
	}
	if !sawDone {
		send(ctx, ch, p.errStop(ctx, "stream interrupted: ended without [DONE]"))
		return
	}
	send(ctx, ch, llm.Stop{Reason: stopReasonOf(finish)})
}

func (p *Provider) httpError(resp *http.Response) llm.Stop {
	snippet := make([]byte, 2048)
	n, _ := io.ReadFull(resp.Body, snippet)
	return llm.Stop{
		Reason:       llm.StopReasonError,
		ErrorMessage: fmt.Sprintf("%s: HTTP %d: %s", p.providerID, resp.StatusCode, strings.TrimSpace(string(snippet[:n]))),
	}
}

// errStop classifies a failure: context cancellation aborts, everything
// else is an error carrying the provider identity.
func (p *Provider) errStop(ctx context.Context, format string, args ...any) llm.Stop {
	msg := fmt.Sprintf(format, args...)
	if ctx.Err() != nil {
		return llm.Stop{Reason: llm.StopReasonAborted, ErrorMessage: msg}
	}
	return llm.Stop{Reason: llm.StopReasonError, ErrorMessage: p.providerID + ": " + msg}
}

// send delivers an event without blocking forever on a consumer that has
// stopped reading because its context was canceled.
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
