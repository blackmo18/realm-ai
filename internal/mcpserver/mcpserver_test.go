package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/blackmo18/realm-ai/internal/runtime"
)

func TestServe(t *testing.T) {
	s := Server{
		Name: "realm", Version: "test",
		Tools: []runtime.ToolDef{{Name: "recall", Description: "d", Schema: map[string]any{"type": "object"}}},
		Call: func(_ context.Context, name string, args json.RawMessage) (string, error) {
			if name == "fail" {
				return "", errors.New("denied by policy")
			}
			return name + ":" + string(args), nil
		},
	}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"recall","arguments":{"query":"x"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fail"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"nope"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":6,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":"bad"}`,
		`{"jsonrpc":"2.0","id":8,"method":"initialize"}`,
	}, "\n")
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 9 {
		t.Fatalf("want 9 responses (notification skipped), got %d:\n%s", len(lines), out.String())
	}
	checks := []string{
		`"protocolVersion":"2025-03-26"`,
		`"inputSchema":{"type":"object"}`,
		`recall:{\"query\":\"x\"}`,
		`"isError":true`,
		`"code":-32601`,
		`"code":-32700`,
		`"id":6,"result":{}`,
		`"code":-32602`,
		`"protocolVersion":"` + defaultProtocol + `"`,
	}
	for i, want := range checks {
		if !strings.Contains(lines[i], want) {
			t.Errorf("response %d missing %s: %s", i, want, lines[i])
		}
	}
	if !strings.Contains(lines[3], "denied by policy") {
		t.Errorf("tool error text missing: %s", lines[3])
	}
}
