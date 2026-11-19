package client_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/client"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/store"
)

type echoRunner struct{}

func (echoRunner) Run(_ context.Context, t runtime.Turn) (runtime.Result, error) {
	if t.ReplaceSystemPrompt {
		return runtime.Result{Text: `{"action":"route","to":["@app/helper"]}`}, nil
	}
	return runtime.Result{Text: "{\"decision\":\"act\"}\nhello back"}, nil
}

// TestDaemonEndToEnd runs the real daemon on a unix socket and drives it
// through the client: chats, mentions, moderator routing, DMs, picks,
// streaming, tools and shutdown.
func TestDaemonEndToEnd(t *testing.T) {
	short, err := os.MkdirTemp("", "rd")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(short)
	home := filepath.Join(short, "h")
	t.Setenv(config.EnvHome, home)
	e, err := engine.New(engine.Options{Config: config.Default(), Global: config.Scope{Name: "global", Root: home},
		Runners: func(bot.Bot) (runtime.Runner, error) { return echoRunner{}, nil }, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	proj := filepath.Join(short, "app")
	_ = os.MkdirAll(proj, 0o755)
	if _, err := e.RegisterProject(proj); err != nil {
		t.Fatal(err)
	}
	repo, _ := e.Repo("app")
	if _, err := repo.Create(bot.Bot{Name: "helper", Charter: "Helps."}); err != nil {
		t.Fatal(err)
	}

	socket := filepath.Join(short, "r.sock")
	ln, err := daemon.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx, ln, e) }()

	c := client.New(socket)
	if err := c.WaitHealthy(ctx, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Listen(socket); err == nil {
		t.Fatal("second daemon on the same socket should fail")
	}

	chat, err := c.CreateChat(ctx, daemon.ChatCreate{Scope: "app", Name: "dev"})
	if err != nil || chat.ID != "app/dev" {
		t.Fatalf("create chat: %+v %v", chat, err)
	}
	if _, err := c.CreateChat(ctx, daemon.ChatCreate{Scope: "app", Name: "BAD"}); err == nil {
		t.Fatal("bad chat accepted")
	}
	stream, err := c.Stream(ctx, chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	posted, err := c.Post(ctx, chat.ID, "can someone help")
	if err != nil {
		t.Fatal(err)
	}
	reply := waitFor(t, stream, func(m store.Message) bool { return m.From == "@app/helper" })
	if reply.Parent != posted.ID || reply.Text != "hello back" {
		t.Fatalf("reply: %+v", reply)
	}

	dm, err := c.DM(ctx, "@helper", "app", "hi")
	if err != nil || !strings.HasPrefix(dm.Chat, "app/dm:") {
		t.Fatalf("dm: %+v %v", dm, err)
	}
	e.Wait()
	msgs, err := c.Messages(ctx, dm.Chat, 10)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("dm messages: %+v %v", msgs, err)
	}

	chats, err := c.ListChats(ctx, "app")
	if err != nil || len(chats) != 2 {
		t.Fatalf("list chats: %+v %v", chats, err)
	}
	if got, err := c.GetChat(ctx, chat.ID); err != nil || got.Routing != store.RoutingModerated {
		t.Fatalf("get chat: %+v %v", got, err)
	}
	if err := c.SetRouting(ctx, chat.ID, store.RoutingMention); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddMember(ctx, chat.ID, "@helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reroute(ctx, chat.ID, posted.ID, "@helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pick(ctx, chat.ID, "", "@helper"); err == nil {
		t.Fatal("pick without a pending question should fail")
	}
	if n, err := c.BotChanged(ctx, "@app/helper"); err != nil || n != 0 {
		t.Fatalf("bot changed: %d %v", n, err)
	}
	if _, err := c.Tool(ctx, "bad-token", "recall", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "turn token") {
		t.Fatalf("tool with bad token: %v", err)
	}
	if _, err := c.Stream(ctx, "app/missing"); err == nil {
		t.Fatal("stream of missing chat should fail")
	}
	if _, err := c.GetChat(ctx, "app/missing"); err == nil {
		t.Fatal("missing chat should fail")
	}

	if err := c.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop")
	}
	e.Wait()
	if err := client.New(socket).Health(context.Background()); err != client.ErrNotRunning {
		t.Fatalf("want ErrNotRunning after shutdown, got %v", err)
	}
}

func waitFor(t *testing.T, ch <-chan store.Message, match func(store.Message) bool) store.Message {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case m, open := <-ch:
			if !open {
				t.Fatal("stream closed")
			}
			if match(m) {
				return m
			}
		case <-timeout:
			t.Fatal("timed out waiting for message")
		}
	}
}
