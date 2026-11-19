package engine

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/ids"
	"github.com/blackmo18/realm-ai/internal/mention"
	"github.com/blackmo18/realm-ai/internal/policy"
	"github.com/blackmo18/realm-ai/internal/store"
)

var chatNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

// ChatID returns the id of a group chat in a scope.
func ChatID(scope, name string) string { return scope + "/" + name }

func dmID(scope, a, b string) string {
	pair := []string{a, b}
	slices.Sort(pair)
	return scope + "/dm:" + pair[0] + "+" + pair[1]
}

func scopeOfChat(id string) string {
	scope, _, _ := strings.Cut(id, "/")
	return scope
}

func (e *Engine) chat(id string) (store.Chat, *store.Store, error) {
	st, err := e.store(scopeOfChat(id))
	if err != nil {
		return store.Chat{}, nil, err
	}
	c, err := st.GetChat(id)
	if err != nil {
		return store.Chat{}, nil, err
	}
	return c, st, nil
}

// GetChat loads a chat by id.
func (e *Engine) GetChat(id string) (store.Chat, error) {
	c, _, err := e.chat(id)
	return c, err
}

// CreateChat creates a group chat. Members are bot handles; with no
// members, every active bot of the scope is routable.
func (e *Engine) CreateChat(scope, name, routing string, members []string) (store.Chat, error) {
	if !chatNameRe.MatchString(name) {
		return store.Chat{}, fmt.Errorf("invalid chat name %q: use lowercase letters, digits and dashes", name)
	}
	if routing == "" {
		routing = store.RoutingModerated
	}
	if routing != store.RoutingModerated && routing != store.RoutingMention {
		return store.Chat{}, fmt.Errorf("invalid routing %q (want %s or %s)", routing, store.RoutingModerated, store.RoutingMention)
	}
	if _, err := e.ScopeByName(scope); err != nil {
		return store.Chat{}, err
	}
	resolved := make([]string, 0, len(members))
	for _, m := range members {
		b, err := e.ResolveBot(m, scope)
		if err != nil {
			return store.Chat{}, err
		}
		if !policy.CanAccessChat(actorOf(b), scope) {
			return store.Chat{}, fmt.Errorf("%s cannot join a chat of scope %q", b.Handle(), scope)
		}
		resolved = append(resolved, b.Handle())
	}
	if routing == store.RoutingModerated {
		if _, err := e.ensureModerator(scope); err != nil {
			return store.Chat{}, fmt.Errorf("create moderator: %w", err)
		}
	}
	st, err := e.store(scope)
	if err != nil {
		return store.Chat{}, err
	}
	c, err := st.CreateChat(store.Chat{ID: ChatID(scope, name), Kind: store.KindGroup, Scope: scope, Name: name, Routing: routing})
	if err != nil {
		return store.Chat{}, err
	}
	for _, h := range resolved {
		if err := st.AddMember(c.ID, h); err != nil {
			return store.Chat{}, err
		}
	}
	return c, nil
}

// AddMember adds a bot to a group chat, enforcing the access policy.
func (e *Engine) AddMember(chatID, handle string) (string, error) {
	c, st, err := e.chat(chatID)
	if err != nil {
		return "", err
	}
	b, err := e.ResolveBot(handle, c.Scope)
	if err != nil {
		return "", err
	}
	if !policy.CanAccessChat(actorOf(b), c.Scope) {
		return "", fmt.Errorf("%s cannot join %s: project bots only join their own project's chats", b.Handle(), c.ID)
	}
	return b.Handle(), st.AddMember(c.ID, b.Handle())
}

// SetRouting switches a chat between moderated and mention-only routing.
func (e *Engine) SetRouting(chatID, routing string) error {
	c, st, err := e.chat(chatID)
	if err != nil {
		return err
	}
	if routing != store.RoutingModerated && routing != store.RoutingMention {
		return fmt.Errorf("invalid routing %q", routing)
	}
	if routing == store.RoutingModerated {
		if _, err := e.ensureModerator(c.Scope); err != nil {
			return err
		}
	}
	return st.SetRouting(chatID, routing)
}

// ListChats lists group chats of a scope.
func (e *Engine) ListChats(scope string) ([]store.Chat, error) {
	st, err := e.store(scope)
	if err != nil {
		return nil, err
	}
	return st.ListChats("")
}

// Messages returns recent messages of a chat.
func (e *Engine) Messages(chatID string, limit int) ([]store.Message, error) {
	_, st, err := e.chat(chatID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	return st.Recent(chatID, limit)
}

// Members returns the bots a chat's moderator may route to: every active
// bot of the chat's scope plus bots added explicitly (for example global
// bots joined to a project chat).
func (e *Engine) Members(c store.Chat) ([]bot.Bot, error) {
	st, err := e.store(c.Scope)
	if err != nil {
		return nil, err
	}
	repo, err := e.Repo(c.Scope)
	if err != nil {
		return nil, err
	}
	candidates, err := repo.List()
	if err != nil {
		return nil, err
	}
	handles, err := st.Members(c.ID)
	if err != nil {
		return nil, err
	}
	for _, h := range handles {
		b, err := e.ResolveBot(h, c.Scope)
		if err != nil || b.Scope == c.Scope {
			continue
		}
		candidates = append(candidates, b)
	}
	out := make([]bot.Bot, 0, len(candidates))
	for _, b := range candidates {
		if b.IsModerator() || b.Status != bot.StatusActive || !policy.CanAccessChat(actorOf(b), c.Scope) {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

func (e *Engine) ensureDM(a, b string, aActor, bActor policy.Actor) (store.Chat, *store.Store, error) {
	scope := policy.DMScope(aActor, bActor)
	st, err := e.store(scope)
	if err != nil {
		return store.Chat{}, nil, err
	}
	id := dmID(scope, a, b)
	if c, err := st.GetChat(id); err == nil {
		return c, st, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.Chat{}, nil, err
	}
	pair := []string{a, b}
	slices.Sort(pair)
	c, err := st.CreateChat(store.Chat{ID: id, Kind: store.KindDM, Scope: scope, Name: pair[0] + "+" + pair[1], Routing: store.RoutingMention})
	if err != nil {
		if existing, getErr := st.GetChat(id); getErr == nil {
			return existing, st, nil
		}
		return store.Chat{}, nil, err
	}
	return c, st, nil
}

// insert stores and publishes a new message.
func (e *Engine) insert(st *store.Store, chatID, from, kind, text string, parent *store.Message, hop int, mentions []string) (store.Message, error) {
	m := store.Message{ID: ids.New(), Chat: chatID, From: from, Kind: kind, Text: text, Hop: hop, Mentions: mentions, TS: time.Now().UTC()}
	m.Thread = m.ID
	if parent != nil {
		m.Parent = parent.ID
		m.Thread = parent.Thread
	}
	if err := st.InsertMessage(m); err != nil {
		return store.Message{}, err
	}
	e.hub.Publish(m)
	return m, nil
}

func (e *Engine) system(st *store.Store, chatID string, parent *store.Message, format string, args ...any) {
	if _, err := e.insert(st, chatID, "@realm", store.MsgSystem, fmt.Sprintf(format, args...), parent, 0, nil); err != nil {
		e.logf("system message in %s: %v", chatID, err)
	}
}

// PostUser posts a user message to a chat and routes it in the background.
func (e *Engine) PostUser(chatID, text string) (store.Message, error) {
	if strings.TrimSpace(text) == "" {
		return store.Message{}, errors.New("message text is empty")
	}
	c, st, err := e.chat(chatID)
	if err != nil {
		return store.Message{}, err
	}
	m, err := e.insert(st, c.ID, bot.UserHandle, store.MsgText, text, nil, 0, mention.Parse(text))
	if err != nil {
		return store.Message{}, err
	}
	if !e.track() {
		return m, errors.New("realm is shutting down")
	}
	go func() {
		defer e.wg.Done()
		e.route(c, st, m)
	}()
	return m, nil
}

// DMUser sends a direct message from the user to a bot.
func (e *Engine) DMUser(handle, ctxScope, text string) (store.Message, error) {
	b, err := e.ResolveBot(handle, ctxScope)
	if err != nil {
		return store.Message{}, err
	}
	if b.IsModerator() {
		return store.Message{}, errors.New("the moderator only routes chat messages; DM a bot instead")
	}
	c, _, err := e.ensureDM(bot.UserHandle, b.Handle(), policy.User(), actorOf(b))
	if err != nil {
		return store.Message{}, err
	}
	return e.PostUser(c.ID, text)
}

// route decides which bots run for a user message. Bots never self-select:
// only DM targets, mentioned bots, or moderator picks run.
func (e *Engine) route(c store.Chat, st *store.Store, m store.Message) {
	switch {
	case c.Kind == store.KindDM:
		for _, h := range strings.Split(c.Name, "+") {
			if h != bot.UserHandle {
				e.enqueue(job{Bot: h, Chat: c.ID, MessageID: m.ID, Text: m.Text})
			}
		}
	case len(m.Mentions) > 0:
		if e.tryPick(c, st, m) {
			return
		}
		e.deliverMentions(c, st, m)
	case c.Routing == store.RoutingModerated:
		e.moderate(c, st, m)
	}
}

func (e *Engine) deliverMentions(c store.Chat, st *store.Store, m store.Message) {
	for _, h := range m.Mentions {
		b, err := e.ResolveBot(h, c.Scope)
		if err != nil {
			e.system(st, c.ID, &m, "%s: no such bot", h)
			continue
		}
		if b.IsModerator() {
			e.system(st, c.ID, &m, "the moderator routes messages automatically; mention a bot instead")
			continue
		}
		if !policy.CanAccessChat(actorOf(b), c.Scope) {
			e.system(st, c.ID, &m, "%s cannot access this chat (project bots stay in their own project)", b.Handle())
			continue
		}
		if err := st.AddMember(c.ID, b.Handle()); err != nil {
			e.logf("add member: %v", err)
		}
		e.enqueue(job{Bot: b.Handle(), Chat: c.ID, MessageID: m.ID, Text: m.Text})
	}
}

// tryPick treats a bare "@bot" reply to a pending moderator question as the
// user's pick.
func (e *Engine) tryPick(c store.Chat, st *store.Store, m store.Message) bool {
	if len(m.Mentions) != 1 {
		return false
	}
	rest := strings.TrimSpace(strings.Replace(m.Text, m.Mentions[0], "", 1))
	if strings.Trim(rest, ".,!? ") != "" {
		return false
	}
	ask, err := st.LatestAsk(c.ID)
	if err != nil {
		return false
	}
	if _, err := e.Pick(c.ID, ask.MessageID, m.Mentions[0]); err != nil {
		e.system(st, c.ID, &m, "could not route: %v", err)
	}
	return true
}

// Pick answers a pending moderator question: the source message goes to the
// chosen bot and the choice is saved as a routing hint.
func (e *Engine) Pick(chatID, askID, handle string) (string, error) {
	c, st, err := e.chat(chatID)
	if err != nil {
		return "", err
	}
	var ask store.Ask
	if askID == "" {
		ask, err = st.LatestAsk(c.ID)
	} else {
		ask, err = st.GetAsk(askID)
	}
	if err != nil {
		return "", err
	}
	src, err := st.GetMessage(ask.SourceID)
	if err != nil {
		return "", err
	}
	target, err := e.routeTo(c, st, src, handle, "picked by user")
	if err != nil {
		return "", err
	}
	if err := st.DeleteAsk(ask.MessageID); err != nil {
		e.logf("delete ask: %v", err)
	}
	return target, nil
}

// Reroute sends an already-posted message to a different bot and saves the
// correction as a routing hint for the moderator.
func (e *Engine) Reroute(chatID, messageID, handle string) (string, error) {
	c, st, err := e.chat(chatID)
	if err != nil {
		return "", err
	}
	src, err := st.GetMessage(messageID)
	if err != nil {
		return "", err
	}
	if src.Chat != c.ID {
		return "", fmt.Errorf("message %s is not in %s", messageID, c.ID)
	}
	return e.routeTo(c, st, src, handle, "rerouted by user")
}

func (e *Engine) routeTo(c store.Chat, st *store.Store, src store.Message, handle, why string) (string, error) {
	b, err := e.ResolveBot(handle, c.Scope)
	if err != nil {
		return "", err
	}
	if b.IsModerator() || !policy.CanAccessChat(actorOf(b), c.Scope) {
		return "", fmt.Errorf("%s cannot handle messages in %s", b.Handle(), c.ID)
	}
	e.system(st, c.ID, &src, "→ %s (%s)", b.Handle(), why)
	e.saveHint(c.Scope, fmt.Sprintf("Messages like %q go to %s.", clip(src.Text, 160), b.Handle()))
	e.enqueue(job{Bot: b.Handle(), Chat: c.ID, MessageID: src.ID, Text: src.Text, Routed: true})
	return b.Handle(), nil
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// DMChatID returns the chat id of a DM between two handles stored in scope.
func DMChatID(scope, a, b string) string { return dmID(scope, a, b) }
