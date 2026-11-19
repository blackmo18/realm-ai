package prompt

import (
	"strings"
	"testing"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/store"
)

func TestSystemProjectBot(t *testing.T) {
	b := bot.Bot{Name: "reviewer", Scope: "app", Charter: "Reviews Go code.", Topics: []string{"go"}}
	s := System(b, "- Style (style-1)\n", Context{ProjectDir: "/code/app", ToolsNote: "realm tools via MCP"})
	for _, want := range []string{
		`{"decision":"act"}`, "You are @app/reviewer.", `bot of project "app"`, "Project folder: /code/app",
		"CHARTER\nReviews Go code.", "Topics: go", "TOOLS\nrealm tools via MCP", "KNOWLEDGE INDEX", "- Style (style-1)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	if strings.Index(s, "RULES") > strings.Index(s, "IDENTITY") {
		t.Fatal("static rules must come first")
	}
}

func TestSystemGlobalBotMinimal(t *testing.T) {
	s := System(bot.Bot{Name: "ops", Scope: "global", Charter: "Deploys."}, "", Context{})
	if !strings.Contains(s, "global bot") || strings.Contains(s, "KNOWLEDGE INDEX") || strings.Contains(s, "TOOLS") || strings.Contains(s, "Project folder") {
		t.Fatalf("unexpected global prompt:\n%s", s)
	}
}

func TestTurn(t *testing.T) {
	chat := store.Chat{ID: "app/dev", Kind: store.KindGroup}
	ctx := []store.Message{{From: "@user", Kind: "text", Text: "hi"}}
	p := Turn(chat, ctx, store.Message{From: "@user", Kind: store.MsgCommand}, "run tests")
	if !strings.Contains(p, "Recent messages in app/dev:\n[text] @user: hi") || !strings.Contains(p, "New command for you in app/dev from @user:\nrun tests") {
		t.Fatalf("turn prompt:\n%s", p)
	}
	dm := Turn(store.Chat{Kind: store.KindDM, Name: "@user+@app/x"}, nil, store.Message{From: "@user"}, "yo")
	if strings.Contains(dm, "Recent") || !strings.Contains(dm, "New text for you in direct messages @user+@app/x") {
		t.Fatalf("dm prompt:\n%s", dm)
	}
}
