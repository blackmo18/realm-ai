package apiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/runtime"
)

var echoTool = runtime.ToolDef{Name: "recall", Description: "recall", Schema: map[string]any{"type": "object"}}

func turn(exec runtime.ToolExec) runtime.Turn {
	return runtime.Turn{Model: "m", SystemPrompt: "sys", Prompt: "hi", APITools: []runtime.ToolDef{echoTool}, Exec: exec}
}

func TestAnthropicToolLoop(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") == "" {
			http.Error(w, "bad request shape", http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			if !strings.Contains(string(body), `"system":"sys"`) || !strings.Contains(string(body), `"input_schema"`) {
				http.Error(w, "missing system or tools", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"stop_reason":"tool_use","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"t1","name":"recall","input":{"q":"x"}}]}`))
			return
		}
		if !strings.Contains(string(body), `"tool_result"`) || !strings.Contains(string(body), "remembered: x") {
			http.Error(w, "missing tool result", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`))
	}))
	defer srv.Close()
	r := Runner{Kind: config.ProviderAnthropic, BaseURL: srv.URL, APIKey: "k"}
	res, err := r.Run(context.Background(), turn(func(_ context.Context, name string, args json.RawMessage) (string, error) {
		var a struct{ Q string }
		_ = json.Unmarshal(args, &a)
		return "remembered: " + a.Q, nil
	}))
	if err != nil || res.Text != "done" || calls.Load() != 2 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, calls.Load())
	}
}

func TestOpenAIToolLoop(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad request shape", http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"recall","arguments":"{}"}}]}}]}`))
			return
		}
		if !strings.Contains(string(body), `"tool_call_id":"c1"`) || !strings.Contains(string(body), "error: nope") {
			http.Error(w, "missing tool message", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"final"}}]}`))
	}))
	defer srv.Close()
	r := Runner{Kind: config.ProviderOpenAICompat, BaseURL: srv.URL, APIKey: "k"}
	res, err := r.Run(context.Background(), turn(func(context.Context, string, json.RawMessage) (string, error) {
		return "", errors.New("nope")
	}))
	if err != nil || res.Text != "final" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestErrorsAndLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fail/v1/messages":
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		case "/loop/v1/messages":
			_, _ = w.Write([]byte(`{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"t","name":"recall","input":{}}]}`))
		case "/empty/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[]}`))
		case "/garbage/chat/completions":
			_, _ = w.Write([]byte(`not json`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if _, err := (Runner{Kind: config.ProviderAnthropic, BaseURL: srv.URL + "/fail"}).Run(ctx, turn(nil)); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("want 429 error, got %v", err)
	}
	loop := Runner{Kind: config.ProviderAnthropic, BaseURL: srv.URL + "/loop", MaxSteps: 2}
	if _, err := loop.Run(ctx, turn(nil)); err == nil || !strings.Contains(err.Error(), "2 tool steps") {
		t.Fatalf("want step limit, got %v", err)
	}
	if _, err := (Runner{Kind: config.ProviderOpenAICompat, BaseURL: srv.URL + "/empty"}).Run(ctx, turn(nil)); err == nil {
		t.Fatal("empty choices should fail")
	}
	if _, err := (Runner{Kind: config.ProviderOpenAICompat, BaseURL: srv.URL + "/garbage"}).Run(ctx, turn(nil)); err == nil {
		t.Fatal("bad json should fail")
	}
	if _, err := (Runner{Kind: "magic"}).Run(ctx, turn(nil)); err == nil {
		t.Fatal("unknown kind should fail")
	}
	if _, err := (Runner{Kind: config.ProviderAnthropic}).Run(ctx, runtime.Turn{}); err == nil {
		t.Fatal("missing model should fail")
	}
}

func TestNewReadsKeyFromEnv(t *testing.T) {
	t.Setenv("REALM_TEST_PROVIDER_KEY", "abc")
	r, err := New(config.ProviderConfig{Type: config.ProviderOpenAICompat, BaseURL: "http://x/v1/", APIKeyEnv: "REALM_TEST_PROVIDER_KEY"})
	if err != nil || r.APIKey != "abc" || r.BaseURL != "http://x/v1" {
		t.Fatalf("New: %+v %v", r, err)
	}
	t.Setenv("REALM_TEST_PROVIDER_KEY", "")
	if _, err := New(config.ProviderConfig{Type: config.ProviderOpenAICompat, BaseURL: "http://x", APIKeyEnv: "REALM_TEST_PROVIDER_KEY"}); err == nil {
		t.Fatal("missing key should fail")
	}
}
