// Package tui is the realm terminal UI (`realm tui`), built on Bubble Tea.
// It talks to the daemon through the API interface so it can be tested
// with a fake.
package tui

import (
	"context"

	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/knowledge"
	"github.com/blackmo18/realm-ai/internal/store"
)

// API is the subset of the daemon client the TUI uses.
type API interface {
	Projects(ctx context.Context) ([]store.Project, error)
	ListChats(ctx context.Context, scope string) ([]store.Chat, error)
	GetChat(ctx context.Context, id string) (store.Chat, error)
	Messages(ctx context.Context, chat string, limit int) ([]store.Message, error)
	Stream(ctx context.Context, chat string) (<-chan store.Message, error)
	Post(ctx context.Context, chat, text string) (store.Message, error)
	Pick(ctx context.Context, chat, ask, bot string) (string, error)
	Reroute(ctx context.Context, chat, msg, bot string) (string, error)
	SetRouting(ctx context.Context, chat, routing string) error
	CreateChat(ctx context.Context, in daemon.ChatCreate) (store.Chat, error)
	ListBots(ctx context.Context, scope string) ([]engine.BotView, error)
	CreateBot(ctx context.Context, in engine.BotInput) (engine.BotView, error)
	SetBotStatus(ctx context.Context, handle, action string) (daemon.StatusResult, error)
	InheritKnowledge(ctx context.Context, handle, from string) (int, error)
	Knowledge(ctx context.Context, handle string) ([]knowledge.Entry, error)
	DM(ctx context.Context, to, scope, text string) (store.Message, error)
	Templates(ctx context.Context) ([]engine.Template, error)
	InspectFolder(ctx context.Context, path string) (engine.FolderInfo, error)
	Onboard(ctx context.Context, in engine.OnboardInput) (engine.OnboardResult, error)
}
