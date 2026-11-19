// Package adminmcp is the realm-admin MCP tool set: it lets Claude Code or
// Claude Desktop manage Realm as the user (bots, chats, knowledge,
// projects) through the daemon.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/knowledge"
	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/store"
)

// API is the daemon client surface the admin tools use.
type API interface {
	Projects(ctx context.Context) ([]store.Project, error)
	RegisterProject(ctx context.Context, in daemon.ProjectAdd) (store.Project, error)
	Onboard(ctx context.Context, in engine.OnboardInput) (engine.OnboardResult, error)
	ListBots(ctx context.Context, scope string) ([]engine.BotView, error)
	GetBot(ctx context.Context, handle, scope string) (engine.BotView, error)
	CreateBot(ctx context.Context, in engine.BotInput) (engine.BotView, error)
	UpdateBot(ctx context.Context, handle string, p engine.BotPatch) (engine.BotView, error)
	SetBotStatus(ctx context.Context, handle, action string) (daemon.StatusResult, error)
	InheritKnowledge(ctx context.Context, handle, from string) (int, error)
	Knowledge(ctx context.Context, handle string) ([]knowledge.Entry, error)
	AddKnowledge(ctx context.Context, handle, title, body string) (knowledge.Entry, error)
	ForgetKnowledge(ctx context.Context, handle, id string) error
	ListChats(ctx context.Context, scope string) ([]store.Chat, error)
	CreateChat(ctx context.Context, in daemon.ChatCreate) (store.Chat, error)
	AddMember(ctx context.Context, chat, bot string) (string, error)
	SetRouting(ctx context.Context, chat, routing string) error
	Post(ctx context.Context, chat, text string) (store.Message, error)
	Messages(ctx context.Context, chat string, limit int) ([]store.Message, error)
	Stream(ctx context.Context, chat string) (<-chan store.Message, error)
	DM(ctx context.Context, to, scope, text string) (store.Message, error)
	Pick(ctx context.Context, chat, ask, bot string) (string, error)
	Reroute(ctx context.Context, chat, msg, bot string) (string, error)
}

func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func list(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

var (
	scopeProp   = str(`"global" or a project name`)
	handleProp  = str("Bot handle, e.g. @global/ops or @my-app/reviewer")
	chatProp    = str("Chat id, e.g. my-app/dev or global/lobby")
	routingProp = map[string]any{"type": "string", "enum": []string{"moderated", "mention"}}
)

// Tools returns the admin tool definitions.
func Tools() []runtime.ToolDef {
	return []runtime.ToolDef{
		{Name: "list_projects", Description: "List projects registered with Realm.", Schema: obj(map[string]any{})},
		{Name: "add_project", Description: "Register a project folder (creates <dir>/.realm) and optionally create starter bots (reviewer, docs, tester) and its first group chat. Only use a folder the user named.",
			Schema: obj(map[string]any{"dir": str("Project folder path (~ allowed)"), "name": str("Project name; defaults to the folder name"),
				"ignore_db": map[string]any{"type": "boolean", "description": "Add .realm/realm.db to the project's .gitignore (writes that file). Default false."},
				"starters":  list("Starter bot templates: reviewer, docs, tester"), "global_members": list("Global bot handles to add to the first chat"),
				"chat_name": str("First chat name (default general)"), "routing": routingProp}, "dir")},
		{Name: "list_bots", Description: "List bots visible from a scope (global shows every project's bots).", Schema: obj(map[string]any{"scope": scopeProp}, "scope")},
		{Name: "get_bot", Description: "Show one bot's definition.", Schema: obj(map[string]any{"handle": handleProp}, "handle")},
		{Name: "create_bot", Description: "Create a bot. The charter is the only work it will act on. Tools default to read-only Read, Grep, Glob; Edit, Write and Bash give it the user's full file and shell access.",
			Schema: obj(map[string]any{"scope": scopeProp, "name": str("lowercase name"), "charter": str("What this bot owns and refuses"),
				"provider": str("claude-cli (subscription, default) or a configured provider"), "model": str("e.g. haiku, sonnet, opus"),
				"effort": str("low, medium, high, xhigh or max"), "topics": list("Routing topics"), "skills": list(`Allowed skills, or ["*"]`),
				"tools": list("Allowed built-in tools"), "inherit": str("Copy knowledge from this bot handle")}, "scope", "name", "charter")},
		{Name: "update_bot", Description: "Change a bot's provider, model, effort, charter, topics, skills or tools. Omitted fields stay unchanged.",
			Schema: obj(map[string]any{"handle": handleProp, "provider": str(""), "model": str(""), "effort": str(""), "charter": str(""),
				"topics": list(""), "skills": list(""), "tools": list("")}, "handle")},
		{Name: "set_bot_status", Description: "Turn a bot on or off, archive it (destructive: it stops taking messages; ask the user first) or restore it. Knowledge is always kept.",
			Schema: obj(map[string]any{"handle": handleProp, "action": map[string]any{"type": "string", "enum": []string{"on", "off", "archive", "restore"}}}, "handle", "action")},
		{Name: "inherit_knowledge", Description: "Copy all knowledge notes from one bot into another.", Schema: obj(map[string]any{"handle": handleProp, "from": str("Source bot handle")}, "handle", "from")},
		{Name: "list_knowledge", Description: "List a bot's knowledge notes.", Schema: obj(map[string]any{"handle": handleProp}, "handle")},
		{Name: "add_knowledge", Description: "Add a knowledge note to a bot.", Schema: obj(map[string]any{"handle": handleProp, "title": str(""), "body": str("")}, "handle", "title", "body")},
		{Name: "forget_knowledge", Description: "Delete a bot's knowledge note permanently (destructive: ask the user first).", Schema: obj(map[string]any{"handle": handleProp, "id": str("Note id")}, "handle", "id")},
		{Name: "list_chats", Description: "List chats in a scope.", Schema: obj(map[string]any{"scope": scopeProp}, "scope")},
		{Name: "create_chat", Description: "Create a group chat. Project chats include every active project bot; add global bots with members.",
			Schema: obj(map[string]any{"scope": scopeProp, "name": str(""), "routing": routingProp, "members": list("Extra bot handles")}, "scope", "name")},
		{Name: "add_chat_member", Description: "Add a bot to a chat (project bots cannot join global or other projects' chats).", Schema: obj(map[string]any{"chat": chatProp, "bot": handleProp}, "chat", "bot")},
		{Name: "set_chat_routing", Description: "Switch a chat between moderated and mention-only routing.", Schema: obj(map[string]any{"chat": chatProp, "routing": routingProp}, "chat", "routing")},
		{Name: "post_message", Description: "Post a message to a chat as the user. Mention bots with @name, or let the moderator route it. Replies arrive asynchronously; use read_messages.",
			Schema: obj(map[string]any{"chat": chatProp, "text": str("")}, "chat", "text")},
		{Name: "read_messages", Description: "Read recent messages of a chat.", Schema: obj(map[string]any{"chat": chatProp, "limit": map[string]any{"type": "integer"}}, "chat")},
		{Name: "dm_bot", Description: "Send a direct message to a bot as the user and wait for its reply.",
			Schema: obj(map[string]any{"handle": handleProp, "text": str(""), "timeout_sec": map[string]any{"type": "integer", "description": "Default 300"}}, "handle", "text")},
		{Name: "pick_bot", Description: "Answer the moderator's pending question in a chat by choosing a bot.", Schema: obj(map[string]any{"chat": chatProp, "bot": handleProp}, "chat", "bot")},
		{Name: "reroute_message", Description: "Send an existing message to a different bot; the moderator learns from it.",
			Schema: obj(map[string]any{"chat": chatProp, "message": str("Message id"), "bot": handleProp}, "chat", "message", "bot")},
	}
}

type args struct {
	Dir           string   `json:"dir"`
	IgnoreDB      bool     `json:"ignore_db"`
	Name          string   `json:"name"`
	Starters      []string `json:"starters"`
	GlobalMembers []string `json:"global_members"`
	ChatName      string   `json:"chat_name"`
	Routing       string   `json:"routing"`
	Scope         string   `json:"scope"`
	Handle        string   `json:"handle"`
	Charter       *string  `json:"charter"`
	Provider      *string  `json:"provider"`
	Model         *string  `json:"model"`
	Effort        *string  `json:"effort"`
	Topics        []string `json:"topics"`
	Skills        []string `json:"skills"`
	Tools         []string `json:"tools"`
	Inherit       string   `json:"inherit"`
	Action        string   `json:"action"`
	From          string   `json:"from"`
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	ID            string   `json:"id"`
	Chat          string   `json:"chat"`
	Members       []string `json:"members"`
	Bot           string   `json:"bot"`
	Text          string   `json:"text"`
	Limit         int      `json:"limit"`
	Message       string   `json:"message"`
	TimeoutSec    int      `json:"timeout_sec"`
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func listPtr(l []string) *[]string {
	if l == nil {
		return nil
	}
	return &l
}

func jsonText(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	return string(data), err
}

// Call returns an MCP caller that runs admin tools against api.
func Call(api API) func(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	return func(ctx context.Context, name string, raw json.RawMessage) (string, error) {
		var a args
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		return dispatch(ctx, api, name, a)
	}
}

func dispatch(ctx context.Context, api API, name string, a args) (string, error) {
	switch name {
	case "list_projects":
		return result(api.Projects(ctx))
	case "add_project":
		return result(api.Onboard(ctx, engine.OnboardInput{Dir: a.Dir, Slug: a.Name, IgnoreDB: a.IgnoreDB, Starters: a.Starters,
			GlobalMembers: a.GlobalMembers, ChatName: a.ChatName, Routing: a.Routing}))
	case "list_bots":
		return result(api.ListBots(ctx, a.Scope))
	case "get_bot":
		return result(api.GetBot(ctx, a.Handle, ""))
	case "create_bot":
		return result(api.CreateBot(ctx, engine.BotInput{Scope: a.Scope, Name: a.Name, Charter: deref(a.Charter), Provider: deref(a.Provider),
			Model: deref(a.Model), Effort: deref(a.Effort), Topics: a.Topics, Skills: a.Skills, Tools: a.Tools, Inherit: a.Inherit}))
	case "update_bot":
		return result(api.UpdateBot(ctx, a.Handle, engine.BotPatch{Provider: a.Provider, Model: a.Model, Effort: a.Effort, Charter: a.Charter,
			Topics: listPtr(a.Topics), Skills: listPtr(a.Skills), Tools: listPtr(a.Tools)}))
	case "set_bot_status":
		return result(api.SetBotStatus(ctx, a.Handle, a.Action))
	case "inherit_knowledge":
		n, err := api.InheritKnowledge(ctx, a.Handle, a.From)
		return fmt.Sprintf("copied %d notes from %s into %s", n, a.From, a.Handle), err
	case "list_knowledge":
		return result(api.Knowledge(ctx, a.Handle))
	case "add_knowledge":
		return result(api.AddKnowledge(ctx, a.Handle, a.Title, a.Body))
	case "forget_knowledge":
		return "forgot " + a.ID, api.ForgetKnowledge(ctx, a.Handle, a.ID)
	case "list_chats":
		return result(api.ListChats(ctx, a.Scope))
	case "create_chat":
		return result(api.CreateChat(ctx, daemon.ChatCreate{Scope: a.Scope, Name: a.Name, Routing: a.Routing, Members: a.Members}))
	case "add_chat_member":
		return result(api.AddMember(ctx, a.Chat, a.Bot))
	case "set_chat_routing":
		return a.Chat + " routing: " + a.Routing, api.SetRouting(ctx, a.Chat, a.Routing)
	case "post_message":
		return result(api.Post(ctx, a.Chat, a.Text))
	case "read_messages":
		limit := a.Limit
		if limit <= 0 {
			limit = 30
		}
		msgs, err := api.Messages(ctx, a.Chat, limit)
		if err != nil {
			return "", err
		}
		return formatMessages(msgs), nil
	case "dm_bot":
		return dmAndWait(ctx, api, a)
	case "pick_bot":
		return result(api.Pick(ctx, a.Chat, "", a.Bot))
	case "reroute_message":
		return result(api.Reroute(ctx, a.Chat, a.Message, a.Bot))
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func result[T any](v T, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return jsonText(v)
}

func formatMessages(msgs []store.Message) string {
	if len(msgs) == 0 {
		return "no messages"
	}
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s %s %s] %s: %s\n", m.ID, m.TS.Format(time.RFC3339), m.Kind, m.From, m.Text)
	}
	return strings.TrimSpace(b.String())
}

// dmAndWait sends a DM and returns the first reply to it.
func dmAndWait(ctx context.Context, api API, a args) (string, error) {
	if strings.TrimSpace(a.Text) == "" {
		return "", errors.New("text is empty")
	}
	b, err := api.GetBot(ctx, a.Handle, "")
	if err != nil {
		return "", err
	}
	timeout := time.Duration(a.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	chatID := engine.DMChatID(b.Scope, bot.UserHandle, b.Handle)
	stream, streamErr := api.Stream(ctx, chatID)
	posted, err := api.DM(ctx, b.Handle, b.Scope, a.Text)
	if err != nil {
		return "", err
	}
	if streamErr != nil { // first DM: the chat did not exist before posting
		if stream, err = api.Stream(ctx, posted.Chat); err != nil {
			return "", err
		}
		if msgs, err := api.Messages(ctx, posted.Chat, 10); err == nil {
			for _, m := range msgs {
				if m.Parent == posted.ID {
					return replyText(m), nil
				}
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("no reply from %s within %s; it will still appear in %s", b.Handle, timeout, posted.Chat)
		case m, open := <-stream:
			if !open {
				return "", errors.New("daemon connection closed")
			}
			if m.Parent == posted.ID {
				return replyText(m), nil
			}
		}
	}
}

func replyText(m store.Message) string {
	if m.Kind == store.MsgSystem || m.Kind == store.MsgRefusal {
		return fmt.Sprintf("(%s from %s) %s", m.Kind, m.From, m.Text)
	}
	return m.Text
}
