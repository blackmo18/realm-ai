package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/knowledge"
	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/store"
)

func TestProjectBotTurnIsContextAwareAndResumesSession(t *testing.T) {
	f := setup(t)
	repo, _ := f.e.Repo("app")
	rv, _ := repo.Get("reviewer")
	if _, err := knowledge.Open(bot.KnowledgeDir(rv)).Remember("Style", "Use table tests."); err != nil {
		t.Fatal(err)
	}
	c := f.chat(t, "app", "dev", store.RoutingMention)
	f.post(t, c.ID, "@reviewer first")
	f.post(t, c.ID, "unrelated chatter")
	f.post(t, c.ID, "@reviewer second")
	calls := f.models.callsFor("@app/reviewer")
	if len(calls) != 2 {
		t.Fatalf("reviewer calls = %d", len(calls))
	}
	first, second := calls[0].Turn, calls[1].Turn
	if first.WorkDir != f.projDir || first.SettingSources != "user,project" || first.Resume || first.SessionID == "" {
		t.Fatalf("first turn setup wrong: %+v", first)
	}
	if !strings.Contains(first.SystemPrompt, "CHARTER\nReviews Go code.") || !strings.Contains(first.SystemPrompt, "Style (") ||
		!strings.Contains(first.SystemPrompt, "Project folder: "+f.projDir) {
		t.Fatalf("system prompt missing charter/knowledge/project:\n%s", first.SystemPrompt)
	}
	if !second.Resume || second.SessionID != "sess-@app/reviewer" {
		t.Fatalf("second turn should resume the stored session: %+v", second)
	}
	if strings.Contains(second.Prompt, "@reviewer first") || !strings.Contains(second.Prompt, "unrelated chatter") {
		t.Fatalf("resumed turn should only carry messages after the bot's last reply:\n%s", second.Prompt)
	}
}

func TestOffBotQueuesAndDrainsWhenTurnedOn(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingMention)
	f.setStatus(t, "app", "db", "off")
	f.post(t, c.ID, "@db migrate")
	if f.models.total() != 0 {
		t.Fatal("off bot ran")
	}
	if _, ok := findMsg(f.messages(t, c.ID), "@realm", store.MsgSystem, "@app/db is off; message queued"); !ok {
		t.Fatal("queue notice missing")
	}
	if n, err := f.e.BotChanged("@app/db"); err != nil || n != 0 {
		t.Fatalf("still off, nothing should drain: %d %v", n, err)
	}
	f.setStatus(t, "app", "db", "on")
	n, err := f.e.BotChanged("@app/db")
	f.e.Wait()
	if err != nil || n != 1 || len(f.models.callsFor("@app/db")) != 1 {
		t.Fatalf("drain: n=%d err=%v calls=%d", n, err, len(f.models.callsFor("@app/db")))
	}
	if _, err := f.e.BotChanged("@ghost"); err == nil {
		t.Fatal("unknown bot should fail")
	}
}

func TestArchivedBotRejects(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingMention)
	f.setStatus(t, "app", "db", "archive")
	f.post(t, c.ID, "@db hi")
	if f.models.total() != 0 {
		t.Fatal("archived bot ran")
	}
	if _, ok := findMsg(f.messages(t, c.ID), "@realm", store.MsgSystem, "archived"); !ok {
		t.Fatal("archived notice missing")
	}
}

func TestRefusalSuggestsOwnerAndTeachesModerator(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingModerated)
	f.models.say(mod, `{"action":"route","to":["@app/reviewer"]}`)
	f.models.say("@app/reviewer", "{\"decision\":\"refuse\",\"reason\":\"not code\"}\nI only review code.")
	f.post(t, c.ID, "change the schema for users")
	m, ok := findMsg(f.messages(t, c.ID), "@app/reviewer", store.MsgRefusal, "I only review code.")
	if !ok || !strings.Contains(m.Text, "Reason: not code") || !strings.Contains(m.Text, "Try @app/db") {
		t.Fatalf("refusal: %+v", m)
	}
	if len(f.models.callsFor("@app/db")) != 0 {
		t.Fatal("refusal must not auto-run the suggested bot")
	}
	hints := f.e.hints("app")
	if len(hints) != 1 || !strings.Contains(hints[0], "@app/reviewer refused") {
		t.Fatalf("hints: %v", hints)
	}
}

func TestDelegateDecisionForwardsWithinHopLimit(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingMention)
	f.models.say("@app/reviewer", "{\"decision\":\"delegate\",\"target\":\"@global/ops\",\"reason\":\"deploys\"}\nPassing to ops.")
	f.post(t, c.ID, "@reviewer deploy the api")
	ops := f.models.callsFor("@global/ops")
	if len(ops) != 1 || !strings.Contains(ops[0].Turn.Prompt, "deploy the api") {
		t.Fatalf("ops should get the request in the same chat: %+v", ops)
	}
	msgs := f.messages(t, c.ID)
	if _, ok := findMsg(msgs, "@realm", store.MsgSystem, "@app/reviewer delegated to @global/ops (deploys)"); !ok {
		t.Fatal("delegation notice missing")
	}
	if m, ok := findMsg(msgs, "@global/ops", store.MsgReply, ""); !ok || m.Hop != 1 {
		t.Fatalf("ops reply hop: %+v", m)
	}
	// Ping-pong stops at the hop limit (2 in tests).
	f.models.say("@global/ops", "{\"decision\":\"delegate\",\"target\":\"@app/db\",\"reason\":\"db\"}")
	f.models.say("@app/db", "{\"decision\":\"delegate\",\"target\":\"@app/reviewer\",\"reason\":\"code\"}")
	f.post(t, c.ID, "@ops loop")
	if _, ok := findMsg(f.messages(t, c.ID), "@realm", store.MsgSystem, "hop limit (2) reached"); !ok {
		t.Fatal("hop limit notice missing")
	}
}

func TestDelegateToPrivateTargetGoesThroughDM(t *testing.T) {
	f := setup(t)
	g := f.chat(t, "global", "lobby", store.RoutingMention)
	f.models.say("@global/ops", "{\"decision\":\"delegate\",\"target\":\"@app/db\",\"reason\":\"schema\"}")
	f.post(t, g.ID, "@ops what does the users table look like")
	db := f.models.callsFor("@app/db")
	if len(db) != 1 {
		t.Fatalf("db calls = %d", len(db))
	}
	if !strings.Contains(db[0].Turn.Prompt, "direct messages @app/db+@global/ops") {
		t.Fatalf("db should be reached by DM, prompt:\n%s", db[0].Turn.Prompt)
	}
	f.models.say("@app/db", "{\"decision\":\"delegate\",\"target\":\"@app/ghost\"}")
	f.models.say("@global/ops", "{\"decision\":\"delegate\",\"target\":\"@app/db\"}")
	f.post(t, g.ID, "@ops again")
	if _, ok := findMsg(f.messages(t, "app/dm:@app/db+@global/ops"), "@realm", store.MsgSystem, "@app/ghost"); !ok {
		t.Fatal("unknown delegation target notice missing")
	}
}

func TestUserDM(t *testing.T) {
	f := setup(t)
	m, err := f.e.DMUser("@reviewer", "app", "hello")
	if err != nil {
		t.Fatal(err)
	}
	f.e.Wait()
	if m.Chat != "app/dm:@app/reviewer+@user" {
		t.Fatalf("dm chat id = %q", m.Chat)
	}
	if _, ok := findMsg(f.messages(t, m.Chat), "@app/reviewer", store.MsgReply, "ok"); !ok {
		t.Fatal("dm reply missing")
	}
	if g, err := f.e.DMUser("@ops", "", "hi"); err != nil || g.Chat != "global/dm:@global/ops+@user" {
		t.Fatalf("global dm: %+v %v", g, err)
	}
	if _, err := f.e.DMUser("@app/moderator", "", "hi"); err == nil {
		t.Fatal("DM to moderator should fail")
	}
	if _, err := f.e.PostUser(m.Chat, "  "); err == nil {
		t.Fatal("empty message accepted")
	}
	f.e.Wait()
}

func TestBotFailureIsReported(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingMention)
	f.models.answer("@app/db", func(runtime.Turn) (string, error) { return "", os.ErrDeadlineExceeded })
	f.post(t, c.ID, "@db hi")
	if _, ok := findMsg(f.messages(t, c.ID), "@realm", store.MsgSystem, "@app/db failed"); !ok {
		t.Fatal("failure notice missing")
	}
}

func TestAPIBotGetsRealmAndFileTools(t *testing.T) {
	f := setup(t)
	f.bot(t, "app", bot.Bot{Name: "gpt", Provider: "openai", Model: "gpt-x", Charter: "Answers questions about the code."})
	if err := os.WriteFile(filepath.Join(f.projDir, "main.go"), []byte("package main // hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	var fileOut, recallOut string
	f.models.answer("@app/gpt", func(turn runtime.Turn) (string, error) {
		fileOut, _ = turn.Exec(context.Background(), "read_file", json.RawMessage(`{"path":"main.go"}`))
		_, _ = turn.Exec(context.Background(), "remember", json.RawMessage(`{"title":"Entry","body":"main.go is the entry point"}`))
		recallOut, _ = turn.Exec(context.Background(), "recall", json.RawMessage(`{"query":"entry"}`))
		return "{\"decision\":\"act\"}\nread it", nil
	})
	c := f.chat(t, "app", "dev", store.RoutingMention)
	f.post(t, c.ID, "@gpt what is main.go")
	calls := f.models.callsFor("@app/gpt")
	if len(calls) != 1 || len(calls[0].Turn.APITools) != 11 || calls[0].Turn.MCPConfig != "" {
		t.Fatalf("api turn: %+v", calls)
	}
	if !strings.Contains(fileOut, "hello") || !strings.Contains(recallOut, "entry point") {
		t.Fatalf("tool results: file=%q recall=%q", fileOut, recallOut)
	}
	if strings.Contains(calls[0].Turn.Prompt, "sess-") {
		t.Fatal("api turn should not use CLI sessions")
	}
}

func TestDefaultRunnerSelection(t *testing.T) {
	cfg := config.Default()
	e := &Engine{cfg: cfg}
	if _, err := e.defaultRunner(bot.Bot{Provider: bot.ProviderClaudeCLI}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.defaultRunner(bot.Bot{Provider: "nope"}); err == nil {
		t.Fatal("unknown provider should fail")
	}
	if _, err := e.defaultRunner(bot.Bot{Provider: "ollama"}); err != nil {
		t.Fatalf("keyless provider: %v", err)
	}
}

func TestMCPConfigWrittenForCLITurns(t *testing.T) {
	f := setup(t)
	f.e.selfBin = "/usr/local/bin/realm"
	f.e.socket = "/tmp/realm.sock"
	var cfgBody []byte
	f.models.answer("@app/db", func(turn runtime.Turn) (string, error) {
		cfgBody, _ = os.ReadFile(turn.MCPConfig)
		return "ok", nil
	})
	c := f.chat(t, "app", "dev", store.RoutingMention)
	f.post(t, c.ID, "@db hi")
	turn := f.models.callsFor("@app/db")[0].Turn
	if !strings.Contains(string(cfgBody), `"command":"/usr/local/bin/realm"`) || !strings.Contains(string(cfgBody), "REALM_TURN_TOKEN") {
		t.Fatalf("mcp config: %s", cfgBody)
	}
	if !strings.Contains(turn.SystemPrompt, "mcp__realm__") {
		t.Fatal("tools note missing")
	}
	if _, err := os.Stat(turn.MCPConfig); !os.IsNotExist(err) {
		t.Fatal("mcp config should be removed after the turn")
	}
}
