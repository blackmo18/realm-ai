package router

import (
	"reflect"
	"strings"
	"testing"

	"github.com/blackmo18/realm-ai/internal/store"
)

var members = []Member{
	{Handle: "@app/reviewer", Charter: "Reviews Go code.", Topics: []string{"go"}},
	{Handle: "@app/db", Charter: "Owns schema\nand migrations."},
	{Handle: "@global/ops", Charter: "Deploys."},
}

func TestShouldModerate(t *testing.T) {
	group := store.Chat{Kind: store.KindGroup, Routing: store.RoutingModerated}
	cases := []struct {
		name string
		chat store.Chat
		msg  store.Message
		want bool
	}{
		{"user no mention", group, store.Message{From: "@user"}, true},
		{"user with mention", group, store.Message{From: "@user", Mentions: []string{"@x"}}, false},
		{"bot message", group, store.Message{From: "@app/x"}, false},
		{"mention-only chat", store.Chat{Kind: store.KindGroup, Routing: store.RoutingMention}, store.Message{From: "@user"}, false},
		{"dm", store.Chat{Kind: store.KindDM, Routing: store.RoutingModerated}, store.Message{From: "@user"}, false},
	}
	for _, c := range cases {
		if got := ShouldModerate(c.chat, c.msg); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	all := []string{"@app/reviewer", "@app/db", "@global/ops"}
	cases := []struct {
		name string
		raw  string
		want Decision
	}{
		{"route one", `{"action":"route","to":[{"bot":"@app/reviewer"}],"reason":"go"}`,
			Decision{Action: ActionRoute, To: []Target{{Bot: "@app/reviewer"}}, Reason: "go"}},
		{"route bare name and string form", "Sure:\n```json\n{\"action\":\"route\",\"to\":[\"db\"]}\n```",
			Decision{Action: ActionRoute, To: []Target{{Bot: "@app/db"}}}},
		{"split capped at two, deduped, unknown dropped", `{"action":"route","to":[{"bot":"@ghost"},{"bot":"@app/db","text":"add index"},{"bot":"@app/db"},{"bot":"@global/ops","text":"deploy"},{"bot":"@app/reviewer"}]}`,
			Decision{Action: ActionRoute, To: []Target{{Bot: "@app/db", Text: "add index"}, {Bot: "@global/ops", Text: "deploy"}}}},
		{"route to unknown only falls back to ask", `{"action":"route","to":[{"bot":"@ghost"}]}`,
			Decision{Action: ActionAsk, Options: all, Question: "Which bot should handle this?", Reason: "moderator could not decide"}},
		{"ask", `{"action":"ask","options":["@app/reviewer","@app/db"],"question":"Code or schema?"}`,
			Decision{Action: ActionAsk, Options: []string{"@app/reviewer", "@app/db"}, Question: "Code or schema?"}},
		{"ask with one option widens to all", `{"action":"ask","options":["@app/db"]}`,
			Decision{Action: ActionAsk, Options: all, Question: "Which bot should handle this?"}},
		{"none", `{"action":"none","reason":"chit-chat"}`, Decision{Action: ActionNone, Reason: "chit-chat"}},
		{"garbage", `I think reviewer`, Decision{Action: ActionAsk, Options: all, Question: "Which bot should handle this?", Reason: "moderator could not decide"}},
		{"unknown action", `{"action":"answer"}`, Decision{Action: ActionAsk, Options: all, Question: "Which bot should handle this?", Reason: "moderator could not decide"}},
	}
	for _, c := range cases {
		if got := Parse(c.raw, members); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", c.name, got, c.want)
		}
	}
	if got := Parse(`{"action":"route","to":["@x"]}`, nil); got.Action != ActionNone {
		t.Errorf("no members: %+v", got)
	}
}

func TestParseAmbiguousBareName(t *testing.T) {
	dup := []Member{{Handle: "@app/helper"}, {Handle: "@global/helper"}}
	got := Parse(`{"action":"route","to":["helper"]}`, dup)
	if got.Action != ActionAsk {
		t.Fatalf("ambiguous bare name should ask, got %+v", got)
	}
}

func TestBuildPrompt(t *testing.T) {
	history := []store.Message{{From: "@user", Text: "earlier\nmessage"}, {From: "@app/db", Text: strings.Repeat("x", 400)}}
	p := BuildPrompt(members, []string{"schema questions go to @app/db"}, history, store.Message{Text: "add an index"})
	for _, want := range []string{
		"MEMBERS\n- @app/reviewer: Reviews Go code. [topics: go]",
		"- @app/db: Owns schema and migrations.",
		"ROUTING HINTS\n- schema questions go to @app/db",
		"@user: earlier message",
		"…",
		"LATEST USER MESSAGE\nadd an index",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Index(p, "MEMBERS") > strings.Index(p, "LATEST USER MESSAGE") {
		t.Fatal("static members must come before the variable message")
	}
	if p := BuildPrompt(members, nil, nil, store.Message{Text: "x"}); strings.Contains(p, "ROUTING HINTS") || strings.Contains(p, "RECENT") {
		t.Fatalf("empty sections rendered: %s", p)
	}
}
