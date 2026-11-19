package engine

import (
	"context"
	"strings"
	"time"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/knowledge"
	"github.com/blackmo18/realm-ai/internal/router"
	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/store"
)

const (
	hintTitle        = "route hint"
	maxHints         = 20
	moderatorHistory = 6
	moderatorTimeout = 2 * time.Minute
)

// moderate runs the chat moderator once for an un-mentioned user message.
// Outcomes: route (only the chosen bots run), ask (no bot runs until the
// user picks), none (no bot runs).
func (e *Engine) moderate(c store.Chat, st *store.Store, m store.Message) {
	members, err := e.Members(c)
	if err != nil {
		e.system(st, c.ID, &m, "could not load chat members: %v", err)
		return
	}
	if len(members) == 0 {
		e.system(st, c.ID, &m, "No bots in this chat yet. Create one with `realm bot create`, or mention a bot.")
		return
	}
	mod, err := e.ensureModerator(c.Scope)
	if err != nil {
		e.system(st, c.ID, &m, "moderator unavailable: %v. Mention a bot directly.", err)
		return
	}
	if mod.Status != bot.StatusActive {
		e.system(st, c.ID, &m, "The moderator is %s, so nothing was routed. Mention a bot directly.", mod.Status)
		return
	}
	runner, err := e.runners(mod)
	if err != nil {
		e.system(st, c.ID, &m, "moderator runtime: %v", err)
		return
	}
	rm := make([]router.Member, len(members))
	for i, b := range members {
		rm[i] = router.Member{Handle: b.Handle(), Charter: b.Charter, Topics: b.Topics}
	}
	turn := runtime.Turn{
		Model: mod.Model, Effort: mod.Effort,
		SystemPrompt: router.SystemPrompt, ReplaceSystemPrompt: true, NoTools: true,
		Prompt:  router.BuildPrompt(rm, e.hints(c.Scope), e.history(st, c.ID, m.ID, moderatorHistory), m),
		WorkDir: bot.WorkspaceDir(mod), SettingSources: "local",
	}
	if err := e.acquire(); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, moderatorTimeout)
	res, err := runner.Run(ctx, turn)
	cancel()
	e.release()
	if err != nil {
		e.system(st, c.ID, &m, "The moderator failed (%v), so nothing was routed. Mention a bot directly.", err)
		return
	}
	e.applyRouting(c, st, m, mod, router.Parse(res.Text, rm))
}

func (e *Engine) applyRouting(c store.Chat, st *store.Store, m store.Message, mod bot.Bot, d router.Decision) {
	switch d.Action {
	case router.ActionRoute:
		for _, t := range d.To {
			reason := d.Reason
			if reason == "" {
				reason = "moderator"
			}
			e.system(st, c.ID, &m, "→ %s (%s)", t.Bot, reason)
			text := t.Text
			if text == "" {
				text = m.Text
			}
			e.enqueue(job{Bot: t.Bot, Chat: c.ID, MessageID: m.ID, Text: text, Routed: true})
		}
	case router.ActionAsk:
		text := d.Question + " Options: " + strings.Join(d.Options, ", ") + ". Reply with one handle, e.g. " + d.Options[0] + "."
		q, err := e.insert(st, c.ID, mod.Handle(), store.MsgQuestion, text, &m, 0, nil)
		if err != nil {
			e.logf("post moderator question: %v", err)
			return
		}
		if err := st.SaveAsk(store.Ask{MessageID: q.ID, ChatID: c.ID, SourceID: m.ID, Options: d.Options}); err != nil {
			e.logf("save ask: %v", err)
		}
	default:
		reason := d.Reason
		if reason == "" {
			reason = "no member bot owns this"
		}
		e.system(st, c.ID, &m, "No bot picked this up (%s). Mention a bot to ask it directly.", reason)
	}
}

// history returns up to n messages before excludeID, oldest first.
func (e *Engine) history(st *store.Store, chatID, excludeID string, n int) []store.Message {
	recent, err := st.Recent(chatID, n+1)
	if err != nil {
		return nil
	}
	out := make([]store.Message, 0, len(recent))
	for _, m := range recent {
		if m.ID != excludeID {
			out = append(out, m)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// hints returns the newest routing hints from the scope moderator's knowledge.
func (e *Engine) hints(scope string) []string {
	mod, err := e.ensureModerator(scope)
	if err != nil {
		return nil
	}
	entries, err := knowledge.Open(bot.KnowledgeDir(mod)).List()
	if err != nil {
		return nil
	}
	var out []string
	for _, en := range entries {
		if en.Title == hintTitle {
			out = append(out, en.Body)
		}
	}
	if len(out) > maxHints {
		out = out[len(out)-maxHints:]
	}
	return out
}

func (e *Engine) saveHint(scope, text string) {
	mod, err := e.ensureModerator(scope)
	if err != nil {
		e.logf("save hint: %v", err)
		return
	}
	if _, err := knowledge.Open(bot.KnowledgeDir(mod)).Remember(hintTitle, text); err != nil {
		e.logf("save hint: %v", err)
	}
}

func (e *Engine) acquire() error {
	select {
	case e.sem <- struct{}{}:
		return nil
	case <-e.ctx.Done():
		return e.ctx.Err()
	}
}

func (e *Engine) release() { <-e.sem }
