// Package apiprovider runs bot turns against HTTP model APIs using an API
// key: the Anthropic Messages API or any OpenAI-compatible chat endpoint.
// It runs its own tool loop with the tools passed in the turn.
package apiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/runtime"
)

const (
	defaultMaxSteps  = 12
	defaultMaxTokens = 4096
)

// Runner calls one provider.
type Runner struct {
	Kind     string // config.ProviderAnthropic or config.ProviderOpenAICompat
	BaseURL  string
	APIKey   string
	HTTP     *http.Client
	MaxSteps int
}

// New builds a runner from provider config, reading the key from the env.
func New(p config.ProviderConfig) (Runner, error) {
	key, err := p.APIKey()
	if err != nil {
		return Runner{}, err
	}
	return Runner{Kind: p.Type, BaseURL: strings.TrimRight(p.BaseURL, "/"), APIKey: key}, nil
}

func (r Runner) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (r Runner) steps() int {
	if r.MaxSteps > 0 {
		return r.MaxSteps
	}
	return defaultMaxSteps
}

// Run executes the turn, looping over tool calls until the model answers.
func (r Runner) Run(ctx context.Context, t runtime.Turn) (runtime.Result, error) {
	if t.Model == "" {
		return runtime.Result{}, errors.New("model is required for API providers")
	}
	switch r.Kind {
	case config.ProviderAnthropic:
		return r.runAnthropic(ctx, t)
	case config.ProviderOpenAICompat:
		return r.runOpenAI(ctx, t)
	default:
		return runtime.Result{}, fmt.Errorf("unknown provider type %q", r.Kind)
	}
}

func (r Runner) post(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("read provider response: %w", err)
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("provider returned %s: %s", resp.Status, msg)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode provider response: %w", err)
	}
	return nil
}

func execTool(ctx context.Context, t runtime.Turn, name string, args json.RawMessage) (string, bool) {
	if t.Exec == nil {
		return "tools are not available in this turn", true
	}
	out, err := t.Exec(ctx, name, args)
	if err != nil {
		return "error: " + err.Error(), true
	}
	return out, false
}

// --- Anthropic Messages API ---

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

type anthropicResponse struct {
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
}

func (r Runner) runAnthropic(ctx context.Context, t runtime.Turn) (runtime.Result, error) {
	msgs := []anthropicMessage{{Role: "user", Content: []anthropicBlock{{Type: "text", Text: t.Prompt}}}}
	tools := make([]map[string]any, 0, len(t.APITools))
	for _, d := range t.APITools {
		tools = append(tools, map[string]any{"name": d.Name, "description": d.Description, "input_schema": d.Schema})
	}
	headers := map[string]string{"x-api-key": r.APIKey, "anthropic-version": "2023-06-01"}
	for step := 0; step < r.steps(); step++ {
		body := map[string]any{"model": t.Model, "max_tokens": defaultMaxTokens, "messages": msgs}
		if t.SystemPrompt != "" {
			body["system"] = t.SystemPrompt
		}
		if len(tools) > 0 {
			body["tools"] = tools
		}
		var resp anthropicResponse
		if err := r.post(ctx, r.BaseURL+"/v1/messages", headers, body, &resp); err != nil {
			return runtime.Result{}, err
		}
		var text []string
		var results []anthropicBlock
		for _, b := range resp.Content {
			switch b.Type {
			case "text":
				text = append(text, b.Text)
			case "tool_use":
				out, isErr := execTool(ctx, t, b.Name, b.Input)
				results = append(results, anthropicBlock{Type: "tool_result", ToolUseID: b.ID, Content: out, IsError: isErr})
			}
		}
		if resp.StopReason != "tool_use" || len(results) == 0 {
			return runtime.Result{Text: strings.Join(text, "\n")}, nil
		}
		msgs = append(msgs, anthropicMessage{Role: "assistant", Content: resp.Content}, anthropicMessage{Role: "user", Content: results})
	}
	return runtime.Result{}, fmt.Errorf("stopped after %d tool steps", r.steps())
}

// --- OpenAI-compatible chat completions ---

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
}

func (r Runner) runOpenAI(ctx context.Context, t runtime.Turn) (runtime.Result, error) {
	var msgs []openAIMessage
	if t.SystemPrompt != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: t.SystemPrompt})
	}
	msgs = append(msgs, openAIMessage{Role: "user", Content: t.Prompt})
	tools := make([]map[string]any, 0, len(t.APITools))
	for _, d := range t.APITools {
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
			"name": d.Name, "description": d.Description, "parameters": d.Schema}})
	}
	headers := map[string]string{}
	if r.APIKey != "" {
		headers["Authorization"] = "Bearer " + r.APIKey
	}
	for step := 0; step < r.steps(); step++ {
		body := map[string]any{"model": t.Model, "messages": msgs}
		if len(tools) > 0 {
			body["tools"] = tools
		}
		var resp openAIResponse
		if err := r.post(ctx, r.BaseURL+"/chat/completions", headers, body, &resp); err != nil {
			return runtime.Result{}, err
		}
		if len(resp.Choices) == 0 {
			return runtime.Result{}, errors.New("provider returned no choices")
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			return runtime.Result{Text: msg.Content}, nil
		}
		msgs = append(msgs, openAIMessage{Role: "assistant", Content: msg.Content, ToolCalls: msg.ToolCalls})
		for _, call := range msg.ToolCalls {
			out, _ := execTool(ctx, t, call.Function.Name, json.RawMessage(call.Function.Arguments))
			msgs = append(msgs, openAIMessage{Role: "tool", ToolCallID: call.ID, Content: out})
		}
	}
	return runtime.Result{}, fmt.Errorf("stopped after %d tool steps", r.steps())
}
