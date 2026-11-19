package adminmcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blackmo18/realm-ai/internal/adminmcp"
	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/client"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/mcpserver"
	"github.com/blackmo18/realm-ai/internal/runtime"
)

type replyRunner struct{}

func (replyRunner) Run(_ context.Context, t runtime.Turn) (runtime.Result, error) {
	if t.ReplaceSystemPrompt {
		return runtime.Result{Text: `{"action":"none","reason":"chit-chat"}`}, nil
	}
	return runtime.Result{Text: "{\"decision\":\"act\"}\npong"}, nil
}

var _ adminmcp.API = (*client.Client)(nil)

func startDaemon(t *testing.T) (*client.Client, string) {
	t.Helper()
	short, err := os.MkdirTemp("", "ra")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	home := filepath.Join(short, "h")
	t.Setenv(config.EnvHome, home)
	e, err := engine.New(engine.Options{Config: config.Default(), Global: config.Scope{Name: "global", Root: home},
		Runners: func(bot.Bot) (runtime.Runner, error) { return replyRunner{}, nil }, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(short, "s.sock")
	ln, err := daemon.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = daemon.Serve(ctx, ln, e); close(done) }()
	t.Cleanup(func() { cancel(); <-done; _ = e.Close() })
	c := client.New(socket)
	if err := c.WaitHealthy(context.Background(), 3*time.Second); err != nil {
		t.Fatal(err)
	}
	return c, short
}

func TestAdminToolsEndToEnd(t *testing.T) {
	c, short := startDaemon(t)
	call := adminmcp.Call(c)
	ctx := context.Background()
	do := func(name, args string) string {
		t.Helper()
		out, err := call(ctx, name, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	dir := filepath.Join(short, "shop")
	_ = os.MkdirAll(dir, 0o755)
	raw, _ := json.Marshal(map[string]any{"dir": dir, "starters": []string{"docs"}, "chat_name": "dev"})
	if out := do("add_project", string(raw)); !strings.Contains(out, `"project": "shop"`) {
		t.Fatalf("add_project: %s", out)
	}
	if out := do("list_projects", `{}`); !strings.Contains(out, "shop") {
		t.Fatalf("list_projects: %s", out)
	}
	if out := do("create_bot", `{"scope":"global","name":"ops","charter":"Deploys.","model":"haiku"}`); !strings.Contains(out, `"handle": "@global/ops"`) {
		t.Fatalf("create_bot: %s", out)
	}
	if out := do("update_bot", `{"handle":"@global/ops","effort":"low","topics":["deploy"]}`); !strings.Contains(out, `"effort": "low"`) || !strings.Contains(out, `"model": "haiku"`) {
		t.Fatalf("update_bot kept/changed wrong fields: %s", out)
	}
	if out := do("get_bot", `{"handle":"@global/ops"}`); !strings.Contains(out, "deploy") {
		t.Fatalf("get_bot: %s", out)
	}
	if out := do("list_bots", `{"scope":"shop"}`); !strings.Contains(out, "@shop/docs") {
		t.Fatalf("list_bots: %s", out)
	}
	do("add_knowledge", `{"handle":"@global/ops","title":"Day","body":"Friday"}`)
	if out := do("inherit_knowledge", `{"handle":"@shop/docs","from":"@global/ops"}`); !strings.Contains(out, "copied 1") {
		t.Fatalf("inherit: %s", out)
	}
	notes := do("list_knowledge", `{"handle":"@global/ops"}`)
	var entries []struct{ ID string }
	_ = json.Unmarshal([]byte(notes), &entries)
	if len(entries) != 1 {
		t.Fatalf("list_knowledge: %s", notes)
	}
	do("forget_knowledge", `{"handle":"@global/ops","id":"`+entries[0].ID+`"}`)
	if out := do("create_chat", `{"scope":"global","name":"lobby","routing":"mention"}`); !strings.Contains(out, "global/lobby") {
		t.Fatalf("create_chat: %s", out)
	}
	do("add_chat_member", `{"chat":"global/lobby","bot":"@global/ops"}`)
	do("set_chat_routing", `{"chat":"global/lobby","routing":"moderated"}`)
	do("post_message", `{"chat":"global/lobby","text":"thanks all"}`)
	if out := do("dm_bot", `{"handle":"@global/ops","text":"ping","timeout_sec":10}`); out != "pong" {
		t.Fatalf("dm_bot first DM: %q", out)
	}
	if out := do("dm_bot", `{"handle":"@global/ops","text":"again","timeout_sec":10}`); out != "pong" {
		t.Fatalf("dm_bot second DM: %q", out)
	}
	time.Sleep(200 * time.Millisecond)
	msgs := do("read_messages", `{"chat":"global/lobby"}`)
	if !strings.Contains(msgs, "thanks all") || !strings.Contains(msgs, "No bot picked this up") {
		t.Fatalf("read_messages: %s", msgs)
	}
	if out := do("set_bot_status", `{"handle":"@global/ops","action":"off"}`); !strings.Contains(out, `"status": "off"`) {
		t.Fatalf("set_bot_status: %s", out)
	}
	for _, bad := range []struct{ name, args string }{
		{"pick_bot", `{"chat":"global/lobby","bot":"@global/ops"}`},
		{"reroute_message", `{"chat":"global/lobby","message":"nope","bot":"@global/ops"}`},
		{"dm_bot", `{"handle":"@global/ops","text":" "}`},
		{"dm_bot", `{"handle":"@ghost","text":"x"}`},
		{"explode", `{}`},
		{"get_bot", `not json`},
	} {
		if _, err := call(ctx, bad.name, json.RawMessage(bad.args)); err == nil {
			t.Errorf("%s %s: expected error", bad.name, bad.args)
		}
	}
	if out := do("list_chats", `{"scope":"shop"}`); !strings.Contains(out, "shop/dev") {
		t.Fatalf("list_chats: %s", out)
	}
}

func TestAdminToolsOverMCP(t *testing.T) {
	c, _ := startDaemon(t)
	srv := mcpserver.Server{Name: "realm-admin", Version: "test", Tools: adminmcp.Tools(), Call: adminmcp.Call(c)}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`,
	}, "\n")
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"name":"dm_bot"`) || !strings.Contains(lines[1], `"isError":false`) {
		t.Fatalf("mcp round trip:\n%s", out.String())
	}
	for _, tool := range adminmcp.Tools() {
		if req, ok := tool.Schema["required"].([]string); !ok || req == nil {
			t.Errorf("%s: required must be an array", tool.Name)
		}
	}
}
