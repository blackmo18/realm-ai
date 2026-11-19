package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/store"
)

const mod = "@app/moderator"

func TestMentionSkipsModerator(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingModerated)
	f.post(t, c.ID, "@reviewer please check auth.go")
	if n := len(f.models.callsFor(mod)); n != 0 {
		t.Fatalf("moderator called %d times for a mentioned message", n)
	}
	if n := len(f.models.callsFor("@app/reviewer")); n != 1 {
		t.Fatalf("reviewer called %d times, want 1", n)
	}
	if f.models.total() != 1 {
		t.Fatalf("total model calls = %d, want 1", f.models.total())
	}
	if _, ok := findMsg(f.messages(t, c.ID), "@app/reviewer", store.MsgReply, "ok from @app/reviewer"); !ok {
		t.Fatal("reviewer reply not posted")
	}
}

func TestUnmentionedMessageCallsModeratorOnceAndRoutesToOneBot(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingModerated)
	f.models.say(mod, `{"action":"route","to":[{"bot":"@app/db"}],"reason":"schema change"}`)
	f.post(t, c.ID, "add an index on users.email")
	if n := len(f.models.callsFor(mod)); n != 1 {
		t.Fatalf("moderator called %d times, want 1", n)
	}
	if len(f.models.callsFor("@app/db")) != 1 || len(f.models.callsFor("@app/reviewer")) != 0 {
		t.Fatal("only @app/db should run")
	}
	modTurn := f.models.callsFor(mod)[0].Turn
	if !modTurn.NoTools || !modTurn.ReplaceSystemPrompt || !strings.Contains(modTurn.Prompt, "@app/reviewer: Reviews Go code.") {
		t.Fatalf("moderator turn not set up as a small routing call: %+v", modTurn)
	}
	if strings.Contains(modTurn.Prompt, "@global/ops") {
		t.Fatal("global bots are not default members of a project chat")
	}
	if _, ok := findMsg(f.messages(t, c.ID), "@realm", store.MsgSystem, "→ @app/db (schema change)"); !ok {
		t.Fatal("routing line not posted")
	}
}

func TestSplitRoutesEachPartToItsOwner(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingModerated)
	f.models.say(mod, `{"action":"route","to":[{"bot":"@app/db","text":"add index"},{"bot":"@app/reviewer","text":"review handler"},{"bot":"@app/db"}]}`)
	f.post(t, c.ID, "add index and review handler")
	db, rv := f.models.callsFor("@app/db"), f.models.callsFor("@app/reviewer")
	if len(db) != 1 || len(rv) != 1 {
		t.Fatalf("db=%d reviewer=%d, want 1 each", len(db), len(rv))
	}
	if !strings.Contains(db[0].Turn.Prompt, "add index") || strings.Contains(db[0].Turn.Prompt, "review handler\n") {
		t.Fatalf("db got the wrong part:\n%s", db[0].Turn.Prompt)
	}
}

func TestAskRunsNoBotUntilUserPicks(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingModerated)
	f.models.say(mod, `{"action":"ask","options":["@app/reviewer","@app/db"],"question":"Code review or schema?"}`)
	src := f.post(t, c.ID, "look at the users table code")
	if f.models.total() != 1 {
		t.Fatalf("only the moderator should have run, got %d calls", f.models.total())
	}
	q, ok := findMsg(f.messages(t, c.ID), mod, store.MsgQuestion, "Code review or schema? Options: @app/reviewer, @app/db")
	if !ok || q.Parent != src.ID {
		t.Fatalf("question not posted: %+v", q)
	}
	f.post(t, c.ID, "@app/db")
	db := f.models.callsFor("@app/db")
	if len(db) != 1 || !strings.Contains(db[0].Turn.Prompt, "look at the users table code") {
		t.Fatalf("pick did not route the original message: %+v", db)
	}
	if len(f.models.callsFor(mod)) != 1 {
		t.Fatal("picking must not call the moderator again")
	}
	// The pick is remembered and fed to the next moderator call.
	f.models.say(mod, `{"action":"none"}`)
	f.post(t, c.ID, "another one")
	last := f.models.callsFor(mod)[1].Turn.Prompt
	if !strings.Contains(last, "ROUTING HINTS") || !strings.Contains(last, "go to @app/db") {
		t.Fatalf("hint missing from moderator prompt:\n%s", last)
	}
}

func TestPickCommandAndReroute(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingModerated)
	f.models.say(mod, `{"action":"ask","options":["@app/reviewer","@app/db"]}`)
	f.post(t, c.ID, "hmm")
	if got, err := f.e.Pick(c.ID, "", "@reviewer"); err != nil || got != "@app/reviewer" {
		t.Fatalf("pick: %q %v", got, err)
	}
	f.e.Wait()
	if len(f.models.callsFor("@app/reviewer")) != 1 {
		t.Fatal("picked bot did not run")
	}
	if _, err := f.e.Pick(c.ID, "", "@reviewer"); err == nil {
		t.Fatal("ask should be consumed after pick")
	}
	msgs := f.messages(t, c.ID)
	if _, err := f.e.Reroute(c.ID, msgs[0].ID, "@db"); err != nil {
		t.Fatal(err)
	}
	f.e.Wait()
	if len(f.models.callsFor("@app/db")) != 1 {
		t.Fatal("reroute did not run target")
	}
	if _, err := f.e.Reroute(c.ID, "missing", "@db"); err == nil {
		t.Fatal("reroute of missing message should fail")
	}
	if _, err := f.e.Reroute(c.ID, msgs[0].ID, "@moderator"); err == nil {
		t.Fatal("reroute to the moderator should fail")
	}
}

func TestNoneAndBadOutputRunNoBot(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingModerated)
	f.models.say(mod, `{"action":"none","reason":"chit-chat"}`)
	f.post(t, c.ID, "thanks!")
	if f.models.total() != 1 {
		t.Fatalf("none: %d calls", f.models.total())
	}
	if _, ok := findMsg(f.messages(t, c.ID), "@realm", store.MsgSystem, "No bot picked this up (chit-chat)"); !ok {
		t.Fatal("none notice missing")
	}
	f.models.say(mod, "I would send it to reviewer")
	f.post(t, c.ID, "something")
	if f.models.total() != 2 {
		t.Fatalf("bad output must not run bots: %d calls", f.models.total())
	}
	if _, ok := findMsg(f.messages(t, c.ID), mod, store.MsgQuestion, "Which bot should handle this?"); !ok {
		t.Fatal("bad output should become a question")
	}
	f.models.answer(mod, func(runtime.Turn) (string, error) { return "", errors.New("rate limited") })
	f.post(t, c.ID, "again")
	if _, ok := findMsg(f.messages(t, c.ID), "@realm", store.MsgSystem, "moderator failed (rate limited)"); !ok {
		t.Fatal("moderator failure notice missing")
	}
}

func TestMentionOnlyChatNeverCallsModerator(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "quiet", store.RoutingMention)
	f.post(t, c.ID, "anyone around?")
	f.post(t, c.ID, "look:\n```\n@reviewer in a code block\n```\n> @db quoted")
	if f.models.total() != 0 {
		t.Fatalf("mention-only chat made %d model calls", f.models.total())
	}
}

func TestBotReplyMentionDoesNotTrigger(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingMention)
	f.models.say("@app/reviewer", "{\"decision\":\"act\"}\nLooks fine. @db should check the migration.")
	f.post(t, c.ID, "@reviewer review")
	if len(f.models.callsFor("@app/db")) != 0 || f.models.total() != 1 {
		t.Fatalf("bot reply mention triggered a bot: %d calls", f.models.total())
	}
}

func TestModeratorOffAndEmptyChat(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingModerated)
	f.setStatus(t, "app", "moderator", "off")
	f.post(t, c.ID, "route me")
	if f.models.total() != 0 {
		t.Fatal("off moderator was called")
	}
	if _, ok := findMsg(f.messages(t, c.ID), "@realm", store.MsgSystem, "moderator is off"); !ok {
		t.Fatal("off notice missing")
	}
	g := f.chat(t, "global", "lobby", store.RoutingModerated, "@global/ops")
	f.setStatus(t, "global", "ops", "archive")
	f.post(t, g.ID, "hello")
	if _, ok := findMsg(f.messages(t, g.ID), "@realm", store.MsgSystem, "No bots in this chat yet"); !ok {
		t.Fatal("empty chat notice missing")
	}
}

func TestMentionPolicyAndUnknownBots(t *testing.T) {
	f := setup(t)
	g := f.chat(t, "global", "lobby", store.RoutingModerated)
	f.post(t, g.ID, "@app/reviewer @ghost @moderator hi")
	msgs := f.messages(t, g.ID)
	for _, want := range []string{"@app/reviewer cannot access this chat", "@ghost: no such bot", "moderator routes messages automatically"} {
		if _, ok := findMsg(msgs, "@realm", store.MsgSystem, want); !ok {
			t.Errorf("missing notice %q", want)
		}
	}
	if f.models.total() != 0 {
		t.Fatal("no bot should run")
	}
	// A global bot may work in a project chat, with the project folder added.
	p := f.chat(t, "app", "dev", store.RoutingMention)
	f.post(t, p.ID, "@ops deploy please")
	ops := f.models.callsFor("@global/ops")
	if len(ops) != 1 || len(ops[0].Turn.AddDirs) != 1 || ops[0].Turn.AddDirs[0] != f.projDir || ops[0].Turn.SettingSources != "user" {
		t.Fatalf("global bot turn in project chat: %+v", ops)
	}
	members, _ := f.e.Members(p)
	found := false
	for _, b := range members {
		found = found || b.Handle() == "@global/ops"
	}
	if !found {
		t.Fatal("mentioned global bot should become a member")
	}
}
