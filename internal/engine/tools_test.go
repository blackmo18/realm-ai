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
	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/store"
)

func (f fixture) token(t *testing.T, handle string) string {
	t.Helper()
	b, err := f.e.ResolveBot(handle, "")
	if err != nil {
		t.Fatal(err)
	}
	tok := "tok-" + handle
	f.e.mu.Lock()
	f.e.turns[tok] = turnCtx{Bot: b, Hop: 0, Chain: []string{b.Handle()}}
	f.e.mu.Unlock()
	return tok
}

func (f fixture) tool(t *testing.T, tok, name, args string) (string, error) {
	t.Helper()
	return f.e.CallTool(context.Background(), tok, name, json.RawMessage(args))
}

func TestKnowledgeTools(t *testing.T) {
	f := setup(t)
	tok := f.token(t, "@app/db")
	out, err := f.tool(t, tok, "remember", `{"title":"Engine","body":"We use Postgres 16"}`)
	if err != nil || !strings.HasPrefix(out, "saved engine-") {
		t.Fatalf("remember: %q %v", out, err)
	}
	id := strings.TrimPrefix(out, "saved ")
	if out, _ := f.tool(t, tok, "recall", `{"query":"postgres"}`); !strings.Contains(out, "Postgres 16") {
		t.Fatalf("recall: %q", out)
	}
	if out, _ := f.tool(t, tok, "forget", `{"id":"`+id+`"}`); out != "forgot "+id {
		t.Fatalf("forget: %q", out)
	}
	if out, _ := f.tool(t, tok, "recall", `{"query":"postgres"}`); out != "nothing found" {
		t.Fatalf("recall after forget: %q", out)
	}
	if _, err := f.tool(t, tok, "remember", `{"title":""}`); err == nil {
		t.Fatal("empty remember accepted")
	}
	if _, err := f.tool(t, tok, "forget", `{"id":"nope"}`); err == nil {
		t.Fatal("forget unknown accepted")
	}
	if _, err := f.tool(t, tok, "explode", `{}`); err == nil {
		t.Fatal("unknown tool accepted")
	}
	if _, err := f.tool(t, tok, "recall", `not json`); err == nil {
		t.Fatal("bad args accepted")
	}
	if _, err := f.tool(t, "bogus", "recall", `{}`); err == nil {
		t.Fatal("unknown token accepted")
	}
}

func TestListBotsRespectsPolicy(t *testing.T) {
	f := setup(t)
	other := filepath.Join(filepath.Dir(f.projDir), "web")
	_ = os.MkdirAll(other, 0o755)
	if _, err := f.e.RegisterProject(other); err != nil {
		t.Fatal(err)
	}
	f.bot(t, "web", bot.Bot{Name: "ui", Charter: "Builds UI."})
	projectView, _ := f.tool(t, f.token(t, "@app/db"), "list_bots", `{}`)
	if !strings.Contains(projectView, "@app/reviewer") || !strings.Contains(projectView, "@global/ops") || strings.Contains(projectView, "@web/ui") {
		t.Fatalf("project bot view:\n%s", projectView)
	}
	if strings.Contains(projectView, "@app/db ") || strings.Contains(projectView, "moderator") {
		t.Fatalf("self or moderator listed:\n%s", projectView)
	}
	globalView, _ := f.tool(t, f.token(t, "@global/ops"), "list_bots", `{}`)
	if !strings.Contains(globalView, "@web/ui") || !strings.Contains(globalView, "(topics: go)") {
		t.Fatalf("global bot view:\n%s", globalView)
	}
}

func TestChatToolsEnforceScope(t *testing.T) {
	f := setup(t)
	g := f.chat(t, "global", "lobby", store.RoutingMention)
	p := f.chat(t, "app", "dev", store.RoutingMention)
	f.post(t, p.ID, "release on friday")
	db, ops := f.token(t, "@app/db"), f.token(t, "@global/ops")

	if _, err := f.tool(t, db, "read_chat", `{"chat":"`+g.ID+`"}`); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("project bot read global chat: %v", err)
	}
	if _, err := f.tool(t, db, "post", `{"chat":"`+g.ID+`","text":"hi"}`); err == nil {
		t.Fatal("project bot posted to global chat")
	}
	out, err := f.tool(t, ops, "read_chat", `{"chat":"`+p.ID+`"}`)
	if err != nil || !strings.Contains(out, "release on friday") {
		t.Fatalf("global bot read project chat: %q %v", out, err)
	}
	if out, err := f.tool(t, db, "post", `{"chat":"`+p.ID+`","text":"migration ready @reviewer"}`); err != nil || !strings.HasPrefix(out, "posted ") {
		t.Fatalf("post: %q %v", out, err)
	}
	f.e.Wait()
	if f.models.total() != 0 {
		t.Fatal("a bot post must not trigger other bots")
	}
	if out, _ := f.tool(t, ops, "read_chat", `{"chat":"`+g.ID+`"}`); out != "no messages" {
		t.Fatalf("empty chat: %q", out)
	}
	if _, err := f.tool(t, db, "post", `{"chat":"`+p.ID+`","text":" "}`); err == nil {
		t.Fatal("empty post accepted")
	}
	if _, err := f.tool(t, db, "read_chat", `{"chat":"app/missing"}`); err == nil {
		t.Fatal("missing chat accepted")
	}
	// DMs are readable only by their participants.
	dm, _ := f.e.DMUser("@reviewer", "app", "secret plan")
	f.e.Wait()
	if _, err := f.tool(t, db, "read_chat", `{"chat":"`+dm.Chat+`"}`); err == nil {
		t.Fatal("non-participant read a DM")
	}
}

func TestSendDMTool(t *testing.T) {
	f := setup(t)
	out, err := f.tool(t, f.token(t, "@app/db"), "send_dm", `{"to":"@reviewer","text":"index added"}`)
	f.e.Wait()
	if err != nil || !strings.Contains(out, "sent to @app/reviewer") {
		t.Fatalf("send_dm: %q %v", out, err)
	}
	rv := f.models.callsFor("@app/reviewer")
	if len(rv) != 1 || !strings.Contains(rv[0].Turn.Prompt, "index added") {
		t.Fatalf("target not run: %+v", rv)
	}
	if _, err := f.tool(t, f.token(t, "@app/db"), "send_dm", `{"to":"@reviewer","text":""}`); err == nil {
		t.Fatal("empty dm accepted")
	}
	if _, err := f.tool(t, f.token(t, "@app/db"), "send_dm", `{"to":"@app/db","text":"me"}`); err == nil {
		t.Fatal("dm to self accepted")
	}
}

func TestDelegateTool(t *testing.T) {
	f := setup(t)
	other := filepath.Join(filepath.Dir(f.projDir), "web")
	_ = os.MkdirAll(other, 0o755)
	_, _ = f.e.RegisterProject(other)
	f.bot(t, "web", bot.Bot{Name: "ui", Charter: "Builds UI."})
	db := f.token(t, "@app/db")

	f.models.say("@global/ops", "{\"decision\":\"act\"}\nDeployed v2.")
	out, err := f.tool(t, db, "delegate", `{"to":"@ops","kind":"command","text":"deploy v2"}`)
	if err != nil || out != "Deployed v2." {
		t.Fatalf("delegate: %q %v", out, err)
	}
	dmMsgs := f.messages(t, "app/dm:@app/db+@global/ops")
	if m, ok := findMsg(dmMsgs, "@app/db", store.MsgCommand, "deploy v2"); !ok || m.Hop != 1 {
		t.Fatalf("command message: %+v", m)
	}
	f.models.say("@global/ops", "{\"decision\":\"refuse\",\"reason\":\"no access\"}")
	if out, _ := f.tool(t, db, "delegate", `{"to":"@ops","text":"drop prod"}`); !strings.Contains(out, "refused: no access") {
		t.Fatalf("refusal: %q", out)
	}
	f.models.say("@global/ops", "{\"decision\":\"delegate\",\"target\":\"@app/reviewer\",\"reason\":\"code\"}")
	if out, _ := f.tool(t, db, "delegate", `{"to":"@ops","text":"review"}`); !strings.Contains(out, "@app/reviewer") {
		t.Fatalf("nested delegate: %q", out)
	}
	if _, err := f.tool(t, db, "delegate", `{"to":"@web/ui","text":"x"}`); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("cross-project delegate: %v", err)
	}
	// Cycle: ops (in chain) delegating back to db.
	opsBot := mustBot(t, f, "@global/ops")
	f.e.mu.Lock()
	f.e.turns["cyc"] = turnCtx{Bot: opsBot, Chain: []string{"@app/db", "@global/ops"}}
	f.e.mu.Unlock()
	if _, err := f.tool(t, "cyc", "delegate", `{"to":"@app/db","text":"x"}`); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle: %v", err)
	}
	f.setStatus(t, config.GlobalScope, "ops", "off")
	if _, err := f.tool(t, db, "delegate", `{"to":"@ops","text":"x"}`); err == nil {
		t.Fatal("delegate to off bot accepted")
	}
	dbBot := mustBot(t, f, "@app/db")
	f.e.mu.Lock()
	f.e.turns["deep"] = turnCtx{Bot: dbBot, Hop: 2}
	f.e.mu.Unlock()
	if _, err := f.tool(t, "deep", "delegate", `{"to":"@reviewer","text":"x"}`); err == nil || !strings.Contains(err.Error(), "hop limit") {
		t.Fatalf("hop limit: %v", err)
	}
	if _, err := f.tool(t, db, "delegate", `{"to":"@moderator","text":"x"}`); err == nil {
		t.Fatal("delegate to moderator accepted")
	}
	if _, err := f.tool(t, db, "delegate", `{"to":"@reviewer","text":""}`); err == nil {
		t.Fatal("empty delegate accepted")
	}
	f.models.answer("@app/reviewer", func(runtime.Turn) (string, error) { return "", os.ErrClosed })
	if _, err := f.tool(t, db, "delegate", `{"to":"@reviewer","text":"x"}`); err == nil {
		t.Fatal("target failure should surface")
	}
}

func mustBot(t *testing.T, f fixture, h string) bot.Bot {
	t.Helper()
	b, err := f.e.ResolveBot(h, "")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestChatManagement(t *testing.T) {
	f := setup(t)
	if _, err := f.e.CreateChat("app", "Bad Name", "", nil); err == nil {
		t.Fatal("bad chat name accepted")
	}
	if _, err := f.e.CreateChat("app", "x", "loud", nil); err == nil {
		t.Fatal("bad routing accepted")
	}
	if _, err := f.e.CreateChat("nope", "x", "", nil); err == nil {
		t.Fatal("unknown scope accepted")
	}
	if _, err := f.e.CreateChat("global", "x", "", []string{"@app/db"}); err == nil {
		t.Fatal("project bot joined a global chat")
	}
	if _, err := f.e.CreateChat("app", "x", "", []string{"@ghost"}); err == nil {
		t.Fatal("unknown member accepted")
	}
	c := f.chat(t, "app", "dev", "", "@global/ops")
	if c.Routing != store.RoutingModerated {
		t.Fatalf("default routing = %q", c.Routing)
	}
	if _, err := f.e.AddMember("global/missing", "@ops"); err == nil {
		t.Fatal("missing chat accepted")
	}
	g := f.chat(t, "global", "lobby", store.RoutingMention)
	if _, err := f.e.AddMember(g.ID, "@app/db"); err == nil {
		t.Fatal("project bot added to global chat")
	}
	if h, err := f.e.AddMember(g.ID, "@ops"); err != nil || h != "@global/ops" {
		t.Fatalf("add member: %q %v", h, err)
	}
	if err := f.e.SetRouting(g.ID, store.RoutingModerated); err != nil {
		t.Fatal(err)
	}
	if err := f.e.SetRouting(g.ID, "loud"); err == nil {
		t.Fatal("bad routing accepted")
	}
	chats, err := f.e.ListChats("app")
	if err != nil || len(chats) != 1 {
		t.Fatalf("list chats: %+v %v", chats, err)
	}
	if got, err := f.e.GetChat(c.ID); err != nil || got.ID != c.ID {
		t.Fatalf("get chat: %+v %v", got, err)
	}
}

func TestScopesAndProjects(t *testing.T) {
	f := setup(t)
	sub := filepath.Join(f.projDir, "internal", "x")
	_ = os.MkdirAll(sub, 0o755)
	sc, err := f.e.ProjectForDir(sub)
	if err != nil || sc.Name != "app" || sc.ProjectDir() != f.projDir {
		t.Fatalf("project for dir: %+v %v", sc, err)
	}
	again, err := f.e.RegisterProject(f.projDir)
	if err != nil || again.Name != "app" {
		t.Fatalf("re-register: %+v %v", again, err)
	}
	stray := filepath.Join(filepath.Dir(f.projDir), "stray")
	_ = os.MkdirAll(filepath.Join(stray, ".realm"), 0o755)
	if _, err := f.e.ProjectForDir(stray); err == nil {
		t.Fatal("unregistered .realm folder accepted")
	}
	if _, err := f.e.RegisterProject(filepath.Dir(f.e.global.Root)); err == nil {
		t.Fatal("home folder registered as project")
	}
	projects, _ := f.e.Projects()
	if len(projects) != 1 {
		t.Fatalf("projects: %+v", projects)
	}
	if _, err := f.e.ResolveBot("@ops", "app"); err != nil {
		t.Fatalf("global fallback: %v", err)
	}
	if _, err := f.e.ResolveBot("@nobody", "app"); err == nil {
		t.Fatal("unknown bot resolved")
	}
	if _, err := f.e.ResolveBot("@nope/x", ""); err == nil {
		t.Fatal("unknown scope resolved")
	}
	if _, err := New(Options{}); err == nil {
		t.Fatal("engine without root accepted")
	}
}

func TestHubSubscribe(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("c")
	h.Publish(store.Message{Chat: "c", Text: "hi"})
	h.Publish(store.Message{Chat: "other"})
	if m := <-ch; m.Text != "hi" {
		t.Fatalf("got %+v", m)
	}
	cancel()
	cancel()
	h.Publish(store.Message{Chat: "c"})
	if _, open := <-ch; open {
		t.Fatal("channel should be closed")
	}
}
