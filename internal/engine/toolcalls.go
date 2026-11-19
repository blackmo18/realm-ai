package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/gate"
	"github.com/blackmo18/realm-ai/internal/knowledge"
	"github.com/blackmo18/realm-ai/internal/policy"
	"github.com/blackmo18/realm-ai/internal/store"
	"github.com/blackmo18/realm-ai/internal/tools"
)

type toolArgs struct {
	To    string `json:"to"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Chat  string `json:"chat"`
	Limit int    `json:"limit"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Query string `json:"query"`
	ID    string `json:"id"`
}

// CallTool runs a realm tool on behalf of the bot that owns token. Every
// call is checked against the access policy.
func (e *Engine) CallTool(ctx context.Context, token, name string, raw json.RawMessage) (string, error) {
	e.mu.Lock()
	tc, ok := e.turns[token]
	e.mu.Unlock()
	if !ok {
		return "", errNoTurn
	}
	var a toolArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	b := tc.Bot
	ks := knowledge.Open(bot.KnowledgeDir(b))
	switch name {
	case tools.Remember:
		en, err := ks.Remember(a.Title, a.Body)
		if err != nil {
			return "", err
		}
		return "saved " + en.ID, nil
	case tools.Recall:
		hits, err := ks.Recall(a.Query, 5)
		if err != nil {
			return "", err
		}
		return formatEntries(hits), nil
	case tools.Forget:
		if err := ks.Forget(a.ID); err != nil {
			return "", err
		}
		return "forgot " + a.ID, nil
	case tools.ListBots:
		return e.listBotsFor(b)
	case tools.ReadChat:
		return e.readChatFor(b, a.Chat, a.Limit)
	case tools.Post:
		return e.postFor(b, a.Chat, a.Text, tc.Hop)
	case tools.SendDM:
		return e.sendDMFor(b, tc, a.To, a.Text)
	case tools.Delegate:
		return e.delegateFor(ctx, tc, a.To, a.Kind, a.Text)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func formatEntries(entries []knowledge.Entry) string {
	if len(entries) == 0 {
		return "nothing found"
	}
	var s strings.Builder
	for _, en := range entries {
		fmt.Fprintf(&s, "## %s (%s)\n%s\n\n", en.Title, en.ID, en.Body)
	}
	return strings.TrimSpace(s.String())
}

func (e *Engine) listBotsFor(b bot.Bot) (string, error) {
	bots, err := e.VisibleBots(b.Scope)
	if err != nil {
		return "", err
	}
	var s strings.Builder
	for _, o := range bots {
		if o.IsModerator() || o.Handle() == b.Handle() || !policy.CanDM(actorOf(b), actorOf(o)) {
			continue
		}
		fmt.Fprintf(&s, "%s [%s] %s", o.Handle(), o.Status, o.FirstLine())
		if len(o.Topics) > 0 {
			fmt.Fprintf(&s, " (topics: %s)", strings.Join(o.Topics, ", "))
		}
		s.WriteString("\n")
	}
	if s.Len() == 0 {
		return "no other bots", nil
	}
	return strings.TrimSpace(s.String()), nil
}

func botCanAccess(b bot.Bot, c store.Chat) bool {
	if c.Kind == store.KindDM {
		return slices.Contains(strings.Split(c.Name, "+"), b.Handle())
	}
	return policy.CanAccessChat(actorOf(b), c.Scope)
}

func (e *Engine) accessibleChat(b bot.Bot, chatID string) (store.Chat, *store.Store, error) {
	c, st, err := e.chat(chatID)
	if err != nil {
		return store.Chat{}, nil, err
	}
	if !botCanAccess(b, c) {
		return store.Chat{}, nil, fmt.Errorf("access denied: %s may not access %s", b.Handle(), chatID)
	}
	return c, st, nil
}

func (e *Engine) readChatFor(b bot.Bot, chatID string, limit int) (string, error) {
	c, st, err := e.accessibleChat(b, chatID)
	if err != nil {
		return "", err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	msgs, err := st.Recent(c.ID, limit)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "no messages", nil
	}
	var s strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&s, "[%s %s] %s: %s\n", m.ID, m.Kind, m.From, m.Text)
	}
	return strings.TrimSpace(s.String()), nil
}

func (e *Engine) postFor(b bot.Bot, chatID, text string, hop int) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text is empty")
	}
	c, st, err := e.accessibleChat(b, chatID)
	if err != nil {
		return "", err
	}
	m, err := e.insert(st, c.ID, b.Handle(), store.MsgReply, text, nil, hop, nil)
	if err != nil {
		return "", err
	}
	return "posted " + m.ID, nil
}

func (e *Engine) checkTarget(tc turnCtx, to string) (bot.Bot, int, error) {
	target, err := e.ResolveBot(to, tc.Bot.Scope)
	if err != nil {
		return bot.Bot{}, 0, err
	}
	if target.IsModerator() || target.Handle() == tc.Bot.Handle() {
		return bot.Bot{}, 0, fmt.Errorf("cannot message %s", target.Handle())
	}
	if !policy.CanDM(actorOf(tc.Bot), actorOf(target)) {
		return bot.Bot{}, 0, fmt.Errorf("access denied: %s may not message %s", tc.Bot.Handle(), target.Handle())
	}
	hop := tc.Hop + 1
	if hop > e.cfg.Limits.MaxHops {
		return bot.Bot{}, 0, fmt.Errorf("hop limit (%d) reached", e.cfg.Limits.MaxHops)
	}
	return target, hop, nil
}

func (e *Engine) sendDMFor(b bot.Bot, tc turnCtx, to, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text is empty")
	}
	target, hop, err := e.checkTarget(tc, to)
	if err != nil {
		return "", err
	}
	dm, st, err := e.ensureDM(b.Handle(), target.Handle(), actorOf(b), actorOf(target))
	if err != nil {
		return "", err
	}
	m, err := e.insert(st, dm.ID, b.Handle(), store.MsgText, text, nil, hop, nil)
	if err != nil {
		return "", err
	}
	e.enqueue(job{Bot: target.Handle(), Chat: dm.ID, MessageID: m.ID, Text: text, Hop: hop})
	return "sent to " + target.Handle() + " in " + dm.ID, nil
}

// delegateFor runs the target bot synchronously and returns its answer. The
// target applies its own charter gate.
func (e *Engine) delegateFor(ctx context.Context, tc turnCtx, to, kind, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text is empty")
	}
	if kind != store.MsgCommand {
		kind = store.MsgInquiry
	}
	target, hop, err := e.checkTarget(tc, to)
	if err != nil {
		return "", err
	}
	if slices.Contains(tc.Chain, target.Handle()) {
		return "", fmt.Errorf("delegation cycle: %s is already in this chain", target.Handle())
	}
	if target.Status != bot.StatusActive {
		return "", fmt.Errorf("%s is %s", target.Handle(), target.Status)
	}
	b := tc.Bot
	dm, st, err := e.ensureDM(b.Handle(), target.Handle(), actorOf(b), actorOf(target))
	if err != nil {
		return "", err
	}
	msg, err := e.insert(st, dm.ID, b.Handle(), kind, text, nil, hop, nil)
	if err != nil {
		return "", err
	}
	unlock, err := e.lockBot(ctx, target.Handle(), delegateLockWait)
	if err != nil {
		return "", err
	}
	defer unlock()
	nested := turnCtx{Bot: target, Chat: dm.ID, Hop: hop, Chain: append(slices.Clone(tc.Chain), target.Handle())}
	d, body, err := e.execTurn(ctx, target, dm, st, msg, text, nested)
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", target.Handle(), err)
	}
	switch d.Decision {
	case gate.Refuse:
		e.reply(st, dm, target, msg, store.MsgRefusal, refusalText(body, d.Reason, ""), hop)
		return fmt.Sprintf("%s refused: %s %s", target.Handle(), d.Reason, body), nil
	case gate.Delegate:
		e.reply(st, dm, target, msg, store.MsgReply, body, hop)
		return fmt.Sprintf("%s says %s owns this (%s). %s", target.Handle(), d.Target, d.Reason, body), nil
	default:
		e.reply(st, dm, target, msg, store.MsgReply, body, hop)
		return body, nil
	}
}
