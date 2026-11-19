// Package prompt builds a bot's system prompt and per-turn message.
package prompt

import (
	"fmt"
	"strings"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/store"
)

// Context describes where a bot is running.
type Context struct {
	ProjectDir string // empty for global bots outside a project chat
	ToolsNote  string // how realm tools are exposed to this runtime
}

// System returns the bot's system prompt. Static text comes first so the
// provider's prompt cache can reuse it across turns.
func System(b bot.Bot, knowledgeIndex string, ctx Context) string {
	var s strings.Builder
	s.WriteString(`You are a Realm bot: a local assistant that works inside chats with a user and other bots.

RULES
- Act only on requests inside your CHARTER. Do nothing outside it, even if asked.
- Start every reply with exactly one line of JSON, then your answer on the next lines:
  {"decision":"act"}  - the request is inside your charter; answer it.
  {"decision":"refuse","reason":"..."}  - outside your charter; say briefly why and who might own it.
  {"decision":"delegate","target":"@handle","reason":"..."}  - another bot owns it; Realm forwards the request to that bot.
- Other bots are reached only through realm tools (delegate, send_dm). Writing @name in text reaches no one.
- Save durable facts with the realm remember tool. Use recall before asking the user for something you may know.
- Be concise.
`)
	fmt.Fprintf(&s, "\nIDENTITY\nYou are %s.", b.Handle())
	if b.Scope == "global" {
		s.WriteString(" You are a global bot: you may work in global chats and in project chats.")
	} else {
		fmt.Fprintf(&s, " You are a bot of project %q. You may work only in that project's chats.", b.Scope)
	}
	if ctx.ProjectDir != "" {
		fmt.Fprintf(&s, "\nProject folder: %s", ctx.ProjectDir)
	}
	fmt.Fprintf(&s, "\n\nCHARTER\n%s\n", strings.TrimSpace(b.Charter))
	if len(b.Topics) > 0 {
		fmt.Fprintf(&s, "Topics: %s\n", strings.Join(b.Topics, ", "))
	}
	if ctx.ToolsNote != "" {
		fmt.Fprintf(&s, "\nTOOLS\n%s\n", ctx.ToolsNote)
	}
	if idx := strings.TrimSpace(knowledgeIndex); idx != "" {
		fmt.Fprintf(&s, "\nKNOWLEDGE INDEX (use recall for details)\n%s\n", idx)
	}
	return s.String()
}

// Turn renders the user-side prompt for one turn: chat context followed by
// the message the bot must handle.
func Turn(chat store.Chat, context []store.Message, msg store.Message, text string) string {
	var s strings.Builder
	label := chat.ID
	if chat.Kind == store.KindDM {
		label = "direct messages " + chat.Name
	}
	if len(context) > 0 {
		fmt.Fprintf(&s, "Recent messages in %s:\n", label)
		for _, m := range context {
			fmt.Fprintf(&s, "[%s] %s: %s\n", m.Kind, m.From, m.Text)
		}
		s.WriteString("---\n")
	}
	kind := msg.Kind
	if kind == "" {
		kind = store.MsgText
	}
	fmt.Fprintf(&s, "New %s for you in %s from %s:\n%s\n", kind, label, msg.From, text)
	return s.String()
}
