// Package tools defines the realm tools bots use to talk to each other and
// manage their knowledge, plus read-only file tools for API-key runtimes.
package tools

import (
	"github.com/blackmo18/realm-ai/internal/runtime"
)

// Realm tool names.
const (
	SendDM   = "send_dm"
	Post     = "post"
	ReadChat = "read_chat"
	ListBots = "list_bots"
	Delegate = "delegate"
	Remember = "remember"
	Recall   = "recall"
	Forget   = "forget"
)

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

// Realm returns the realm tool definitions.
func Realm() []runtime.ToolDef {
	return []runtime.ToolDef{
		{Name: Delegate, Description: "Pass a command or inquiry to another bot that owns it and wait for its answer. The target only acts if the request is inside its charter.",
			Schema: obj(map[string]any{
				"to":   str("Target bot handle, e.g. @global/ops or @reviewer"),
				"kind": map[string]any{"type": "string", "enum": []string{"command", "inquiry"}},
				"text": str("The request for the target bot"),
			}, "to", "text")},
		{Name: SendDM, Description: "Send a direct message to another bot without waiting for an answer.",
			Schema: obj(map[string]any{"to": str("Target bot handle"), "text": str("Message text")}, "to", "text")},
		{Name: Post, Description: "Post a message to a chat you can access. Does not trigger other bots.",
			Schema: obj(map[string]any{"chat": str("Chat id, e.g. app/dev or global/lobby"), "text": str("Message text")}, "chat", "text")},
		{Name: ReadChat, Description: "Read recent messages of a chat you can access.",
			Schema: obj(map[string]any{"chat": str("Chat id"), "limit": map[string]any{"type": "integer", "description": "Max messages (default 20)"}}, "chat")},
		{Name: ListBots, Description: "List bots you can reach, with their charters.", Schema: obj(map[string]any{})},
		{Name: Remember, Description: "Save a durable fact to your persistent knowledge.",
			Schema: obj(map[string]any{"title": str("Short title"), "body": str("The fact")}, "title", "body")},
		{Name: Recall, Description: "Search your persistent knowledge.",
			Schema: obj(map[string]any{"query": str("Words to search for")}, "query")},
		{Name: Forget, Description: "Delete one knowledge entry by id.",
			Schema: obj(map[string]any{"id": str("Entry id from the knowledge index")}, "id")},
	}
}
