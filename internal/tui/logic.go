package tui

import (
	"regexp"
	"sort"
	"strings"

	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/store"
)

var (
	mentionTailRe = regexp.MustCompile(`(?:^|\s)@([a-z0-9/-]*)$`)
	optionsRe     = regexp.MustCompile(`Options: ([^.]+)\.`)
)

// mentionQuery returns the partial @handle at the end of text.
func mentionQuery(text string) (string, bool) {
	m := mentionTailRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// suggestions lists bots matching a partial mention, active first, max 6.
func suggestions(bots []engine.BotView, q string) []engine.BotView {
	var out []engine.BotView
	for _, b := range bots {
		if b.Role == "moderator" || b.Status == "archived" {
			continue
		}
		if strings.HasPrefix(b.Name, q) || strings.HasPrefix(strings.TrimPrefix(b.Handle, "@"), q) {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].Status != "active", out[j].Status != "active"
		if ai != aj {
			return !ai
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func applyMention(text, handle string) string {
	i := strings.LastIndex(text, "@")
	if i < 0 {
		return text
	}
	return text[:i] + handle + " "
}

// pendingQuestion returns the newest moderator question that no later
// message answered, and its options.
func pendingQuestion(msgs []store.Message) (store.Message, []string, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		q := msgs[i]
		if q.Kind != store.MsgQuestion {
			continue
		}
		for _, later := range msgs[i+1:] {
			if later.Parent == q.Parent && (later.Kind == store.MsgReply || later.Kind == store.MsgSystem) {
				return store.Message{}, nil, false
			}
		}
		m := optionsRe.FindStringSubmatch(q.Text)
		if m == nil {
			return store.Message{}, nil, false
		}
		var opts []string
		for _, o := range strings.Split(m[1], ",") {
			if o = strings.TrimSpace(o); strings.HasPrefix(o, "@") {
				opts = append(opts, o)
			}
		}
		return q, opts, len(opts) > 0
	}
	return store.Message{}, nil, false
}

// lastUserMessage is the target of a reroute.
func lastUserMessage(msgs []store.Message) (store.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].From == "@user" {
			return msgs[i], true
		}
	}
	return store.Message{}, false
}

func canJoin(botScope, chatScope string) bool {
	return botScope == "global" || botScope == chatScope
}

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func appendMsg(msgs []store.Message, m store.Message) []store.Message {
	for _, x := range msgs {
		if x.ID == m.ID {
			return msgs
		}
	}
	out := append(append([]store.Message(nil), msgs...), m)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
