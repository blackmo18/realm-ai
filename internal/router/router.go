// Package router holds the chat moderator's routing logic: when to call it,
// what to send it, and how to read its decision. The moderator routes a
// message to the bot that owns it, or asks the user when ownership is
// ambiguous. It never answers questions itself.
package router

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/blackmo18/realm-ai/internal/store"
)

// Actions the moderator can take.
const (
	ActionRoute = "route"
	ActionAsk   = "ask"
	ActionNone  = "none"
)

// MaxTargets caps how many bots one message can be routed to.
const MaxTargets = 2

// Member is a routable bot in a chat.
type Member struct {
	Handle  string
	Charter string
	Topics  []string
}

// Target is one routed bot and, for split requests, its part of the message.
type Target struct {
	Bot  string `json:"bot"`
	Text string `json:"text,omitempty"`
}

// Decision is the parsed moderator output.
type Decision struct {
	Action   string
	To       []Target
	Options  []string
	Question string
	Reason   string
}

// ShouldModerate reports whether a message needs the moderator: a user
// message without mentions in a moderated group chat.
func ShouldModerate(chat store.Chat, msg store.Message) bool {
	return chat.Kind == store.KindGroup &&
		chat.Routing == store.RoutingModerated &&
		msg.From == "@user" &&
		len(msg.Mentions) == 0
}

// SystemPrompt is the moderator's fixed system prompt (static, cacheable).
const SystemPrompt = `You are the moderator of a chat room of AI bots. You never answer the user yourself.
Your only job: decide which bot (or bots) owns the user's latest message, based on each bot's charter.

Reply with exactly one JSON object and nothing else, in one of these shapes:
{"action":"route","to":[{"bot":"@handle"}],"reason":"short reason"}
{"action":"route","to":[{"bot":"@a","text":"part for a"},{"bot":"@b","text":"part for b"}],"reason":"split request"}
{"action":"ask","options":["@a","@b"],"question":"short question to the user","reason":"why it is ambiguous"}
{"action":"none","reason":"why no bot owns it"}

Rules:
- Route only to bots listed in MEMBERS. Use their exact handles.
- Route to one bot when one charter clearly covers the message.
- Split to at most 2 bots only when the message holds clearly separate requests with different owners; give each bot only its part.
- Ask when two or more charters overlap for the same request, or the message is too vague to pick an owner.
- Use none for greetings, thanks, chit-chat, or requests no charter covers.
- Follow ROUTING HINTS: they are corrections from the user.`

// BuildPrompt renders the per-message prompt for the moderator. Static
// content (members, hints) comes before the variable message.
func BuildPrompt(members []Member, hints []string, history []store.Message, msg store.Message) string {
	var b strings.Builder
	b.WriteString("MEMBERS\n")
	for _, m := range members {
		fmt.Fprintf(&b, "- %s: %s", m.Handle, oneLine(m.Charter))
		if len(m.Topics) > 0 {
			fmt.Fprintf(&b, " [topics: %s]", strings.Join(m.Topics, ", "))
		}
		b.WriteString("\n")
	}
	if len(hints) > 0 {
		b.WriteString("\nROUTING HINTS\n")
		for _, h := range hints {
			fmt.Fprintf(&b, "- %s\n", oneLine(h))
		}
	}
	if len(history) > 0 {
		b.WriteString("\nRECENT MESSAGES\n")
		for _, m := range history {
			fmt.Fprintf(&b, "%s: %s\n", m.From, clip(oneLine(m.Text), 300))
		}
	}
	fmt.Fprintf(&b, "\nLATEST USER MESSAGE\n%s\n", msg.Text)
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type rawDecision struct {
	Action   string            `json:"action"`
	To       []json.RawMessage `json:"to"`
	Options  []string          `json:"options"`
	Question string            `json:"question"`
	Reason   string            `json:"reason"`
}

// Parse reads moderator output. Unknown handles are dropped; anything
// unusable becomes an "ask" over all members so no bot runs on a guess.
func Parse(raw string, members []Member) Decision {
	if len(members) == 0 {
		return Decision{Action: ActionNone, Reason: "no bots in this chat"}
	}
	handles := make([]string, len(members))
	for i, m := range members {
		handles[i] = m.Handle
	}
	fallback := Decision{Action: ActionAsk, Options: handles, Question: "Which bot should handle this?", Reason: "moderator could not decide"}
	var rd rawDecision
	if err := json.Unmarshal([]byte(extractJSON(raw)), &rd); err != nil {
		return fallback
	}
	switch rd.Action {
	case ActionNone:
		return Decision{Action: ActionNone, Reason: rd.Reason}
	case ActionRoute:
		targets := parseTargets(rd.To, handles)
		if len(targets) == 0 {
			return fallback
		}
		return Decision{Action: ActionRoute, To: targets, Reason: rd.Reason}
	case ActionAsk:
		opts := filterHandles(rd.Options, handles)
		if len(opts) < 2 {
			opts = handles
		}
		q := rd.Question
		if strings.TrimSpace(q) == "" {
			q = fallback.Question
		}
		return Decision{Action: ActionAsk, Options: opts, Question: q, Reason: rd.Reason}
	default:
		return fallback
	}
}

func parseTargets(raw []json.RawMessage, handles []string) []Target {
	var out []Target
	seen := map[string]bool{}
	for _, r := range raw {
		var t Target
		if err := json.Unmarshal(r, &t); err != nil {
			var s string
			if json.Unmarshal(r, &s) != nil {
				continue
			}
			t = Target{Bot: s}
		}
		h := matchHandle(t.Bot, handles)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, Target{Bot: h, Text: strings.TrimSpace(t.Text)})
		if len(out) == MaxTargets {
			break
		}
	}
	return out
}

func filterHandles(in, handles []string) []string {
	var out []string
	for _, h := range in {
		if m := matchHandle(h, handles); m != "" && !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out
}

// matchHandle accepts a full handle or a bare name that matches exactly one
// member.
func matchHandle(h string, handles []string) string {
	h = "@" + strings.TrimPrefix(strings.TrimSpace(h), "@")
	if slices.Contains(handles, h) {
		return h
	}
	var match string
	for _, full := range handles {
		if strings.HasSuffix(full, "/"+strings.TrimPrefix(h, "@")) {
			if match != "" {
				return ""
			}
			match = full
		}
	}
	return match
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return s
	}
	return s[start : end+1]
}
