package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/gate"
	"github.com/blackmo18/realm-ai/internal/ids"
	"github.com/blackmo18/realm-ai/internal/knowledge"
	"github.com/blackmo18/realm-ai/internal/policy"
	"github.com/blackmo18/realm-ai/internal/prompt"
	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/store"
	"github.com/blackmo18/realm-ai/internal/tools"
)

// job is one queued bot turn.
type job struct {
	Bot       string `json:"bot"`
	Chat      string `json:"chat"`
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
	Hop       int    `json:"hop"`
	Routed    bool   `json:"routed"`
	// Root is the message that started this chain of turns (budget key).
	Root string `json:"root"`
}

// turnCtx is the identity behind a turn token used by realm tool calls.
type turnCtx struct {
	Bot   bot.Bot
	Chat  string
	Hop   int
	Chain []string
	Root  string
	// fanout counts send_dm/delegate calls made in this turn.
	fanout *atomic.Int32
}

const (
	delegateLockWait = 60 * time.Second
	// maxFanoutPerTurn caps send_dm + delegate calls in one bot turn.
	maxFanoutPerTurn = 3
	// maxTurnsPerMessage caps bot turns started by one user message,
	// including every delegation and DM it leads to.
	maxTurnsPerMessage = 12
)

func newTurnCtx(b bot.Bot, chat string, hop int, chain []string, root string) turnCtx {
	return turnCtx{Bot: b, Chat: chat, Hop: hop, Chain: chain, Root: root, fanout: &atomic.Int32{}}
}

func (e *Engine) enqueue(j job) {
	if j.Root == "" {
		j.Root = j.MessageID
	}
	if !e.track() {
		return
	}
	go func() {
		defer e.wg.Done()
		e.runJob(j)
	}()
}

func (e *Engine) runJob(j job) {
	c, st, err := e.chat(j.Chat)
	if err != nil {
		e.logf("job for %s: %v", j.Bot, err)
		return
	}
	trigger, err := st.GetMessage(j.MessageID)
	if err != nil {
		e.logf("job for %s: %v", j.Bot, err)
		return
	}
	b, err := e.ResolveBot(j.Bot, c.Scope)
	if err != nil {
		e.system(st, c.ID, &trigger, "%s: %v", j.Bot, err)
		return
	}
	switch b.Status {
	case bot.StatusOff:
		e.queue(b, j, st, c, trigger)
		return
	case bot.StatusArchived:
		e.system(st, c.ID, &trigger, "%s is archived and does not take messages.", b.Handle())
		return
	}
	if !e.spend(j.Root) {
		e.system(st, c.ID, &trigger, "Stopped: this message already used its budget of %d bot turns.", maxTurnsPerMessage)
		return
	}
	// Take the bot lock before a concurrency slot so a busy bot's queue
	// never holds slots other bots need.
	unlock, err := e.lockBot(e.ctx, b.Handle(), 0)
	if err != nil {
		return
	}
	defer unlock()
	if err := e.acquire(); err != nil {
		return
	}
	defer e.release()
	d, body, err := e.execTurn(e.ctx, b, c, st, trigger, j.Text, newTurnCtx(b, c.ID, j.Hop, []string{b.Handle()}, j.Root))
	if err != nil {
		e.system(st, c.ID, &trigger, "%s failed: %v", b.Handle(), err)
		return
	}
	e.handleDecision(b, c, st, trigger, j, d, body)
}

func (e *Engine) queue(b bot.Bot, j job, st *store.Store, c store.Chat, trigger store.Message) {
	bst, err := e.store(b.Scope)
	if err == nil {
		var payload []byte
		if payload, err = json.Marshal(j); err == nil {
			err = bst.PushInbox(b.Handle(), payload)
		}
	}
	if err != nil {
		e.system(st, c.ID, &trigger, "%s is off and the message could not be queued: %v", b.Handle(), err)
		return
	}
	e.system(st, c.ID, &trigger, "%s is off; message queued until it is turned on.", b.Handle())
}

// BotChanged re-runs queued work for a bot that was turned back on. It
// returns how many queued messages were dispatched.
func (e *Engine) BotChanged(handle string) (int, error) {
	b, err := e.ResolveBot(handle, "")
	if err != nil {
		return 0, err
	}
	if b.Status != bot.StatusActive {
		return 0, nil
	}
	bst, err := e.store(b.Scope)
	if err != nil {
		return 0, err
	}
	items, err := bst.DrainInbox(b.Handle())
	if err != nil {
		return 0, err
	}
	for _, raw := range items {
		var j job
		if err := json.Unmarshal(raw, &j); err != nil {
			e.logf("bad inbox item for %s: %v", b.Handle(), err)
			continue
		}
		e.enqueue(j)
	}
	return len(items), nil
}

// lockBot serializes turns per bot. wait <= 0 waits until ctx is done.
func (e *Engine) lockBot(ctx context.Context, handle string, wait time.Duration) (func(), error) {
	e.mu.Lock()
	ch, ok := e.botLocks[handle]
	if !ok {
		ch = make(chan struct{}, 1)
		e.botLocks[handle] = ch
	}
	e.mu.Unlock()
	if wait > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("%s is busy: %w", handle, ctx.Err())
	}
}

func (e *Engine) projectDir(scope string) string {
	if scope == config.GlobalScope || scope == "" {
		return ""
	}
	sc, err := e.ScopeByName(scope)
	if err != nil {
		return ""
	}
	return sc.ProjectDir()
}

// contextFor returns the chat messages a bot sees before the trigger. With a
// resumed CLI session the bot already remembers its earlier turns, so only
// messages after its last reply are sent.
func (e *Engine) contextFor(st *store.Store, c store.Chat, b bot.Bot, trigger store.Message, resumed bool) []store.Message {
	n := e.cfg.Limits.HistoryMessages
	if n <= 0 {
		n = 20
	}
	msgs := e.history(st, c.ID, trigger.ID, n)
	if !resumed {
		return msgs
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].From == b.Handle() {
			return msgs[i+1:]
		}
	}
	return msgs
}

func (e *Engine) execTurn(ctx context.Context, b bot.Bot, c store.Chat, st *store.Store, trigger store.Message, text string, tc turnCtx) (gate.Decision, string, error) {
	runner, err := e.runners(b)
	if err != nil {
		return gate.Decision{}, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, e.turnTimeout())
	defer cancel()
	isCLI := b.Provider == bot.ProviderClaudeCLI
	sessionID, err := st.Session(b.Handle(), c.ID)
	if err != nil {
		return gate.Decision{}, "", err
	}
	index, _ := knowledge.Open(bot.KnowledgeDir(b)).Index()
	projectDir := e.projectDir(c.Scope)

	token := ids.Token()
	e.mu.Lock()
	e.turns[token] = tc
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.turns, token)
		e.mu.Unlock()
	}()

	base := runtime.Turn{
		Model: b.Model, Effort: b.Effort,
		Prompt: prompt.Turn(c, e.contextFor(st, c, b, trigger, isCLI && sessionID != ""), trigger, text),
	}
	var turn runtime.Turn
	var note string
	cleanup := func() {}
	if isCLI {
		turn, note, cleanup, err = e.cliTurn(base, b, projectDir, sessionID, token)
		if err != nil {
			return gate.Decision{}, "", err
		}
	} else {
		turn, note = e.apiTurn(base, projectDir, token)
	}
	defer cleanup()
	turn.SystemPrompt = prompt.System(b, index, prompt.Context{ProjectDir: projectDir, ToolsNote: note})

	res, err := runner.Run(ctx, turn)
	if err != nil {
		return gate.Decision{}, "", err
	}
	if isCLI {
		sid := res.SessionID
		if sid == "" {
			sid = turn.SessionID
		}
		if sid != "" {
			if err := st.SetSession(b.Handle(), c.ID, sid); err != nil {
				e.logf("save session: %v", err)
			}
		}
	}
	d, body := gate.Parse(res.Text)
	return d, body, nil
}

func (e *Engine) cliTurn(base runtime.Turn, b bot.Bot, projectDir, sessionID, token string) (runtime.Turn, string, func(), error) {
	t := base
	t.BuiltinTools = b.Tools
	t.Skills = b.Skills
	t.Disallowed = e.denyRealmFiles(projectDir)
	switch {
	case b.Scope != config.GlobalScope:
		t.WorkDir, t.SettingSources = projectDir, "user,project"
	case projectDir != "":
		t.WorkDir, t.SettingSources, t.AddDirs = bot.WorkspaceDir(b), "user", []string{projectDir}
	default:
		t.WorkDir, t.SettingSources = bot.WorkspaceDir(b), "user"
	}
	if sessionID != "" {
		t.SessionID, t.Resume = sessionID, true
	} else {
		t.SessionID = ids.UUID()
	}
	if e.selfBin == "" {
		return t, "", func() {}, nil
	}
	path, err := e.writeMCPConfig(token)
	if err != nil {
		return runtime.Turn{}, "", nil, err
	}
	t.MCPConfig = path
	note := "Realm tools are MCP tools named mcp__realm__<tool>: delegate, send_dm, post, read_chat, list_bots, remember, recall, forget."
	return t, note, func() { _ = os.Remove(path) }, nil
}

// denyRealmFiles blocks file tools from Realm's own folders (databases,
// turn tokens, other bots' knowledge). Bots use realm tools for knowledge.
func (e *Engine) denyRealmFiles(projectDir string) []string {
	dirs := []string{e.global.Root}
	if projectDir != "" {
		dirs = append(dirs, filepath.Join(projectDir, config.DirName))
	}
	var rules []string
	for _, d := range dirs {
		abs := "//" + strings.TrimPrefix(filepath.ToSlash(d), "/") + "/**"
		for _, tool := range []string{"Read", "Edit", "Write"} {
			rules = append(rules, tool+"("+abs+")")
		}
	}
	return rules
}

func (e *Engine) writeMCPConfig(token string) (string, error) {
	dir := filepath.Join(e.global.Root, "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	cfg := map[string]any{"mcpServers": map[string]any{"realm": map[string]any{
		"type": "stdio", "command": e.selfBin, "args": []string{"mcp"},
		"env": map[string]string{"REALM_TURN_TOKEN": token, "REALM_SOCKET": e.socket, "REALM_HOME": e.global.Root},
	}}}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mcp-"+token[:16]+".json")
	return path, os.WriteFile(path, data, 0o600)
}

func (e *Engine) apiTurn(base runtime.Turn, projectDir, token string) (runtime.Turn, string) {
	t := base
	t.WorkDir = projectDir
	t.APITools = tools.Realm()
	note := "Realm tools: delegate, send_dm, post, read_chat, list_bots, remember, recall, forget."
	if projectDir != "" {
		t.APITools = append(t.APITools, tools.FileDefs()...)
		note += " File tools (read-only, project folder only): read_file, grep, glob."
	}
	files := tools.Files{Root: projectDir}
	t.Exec = func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		if tools.IsFileTool(name) {
			return files.Exec(name, args)
		}
		return e.CallTool(ctx, token, name, args)
	}
	return t, note
}

func (e *Engine) reply(st *store.Store, c store.Chat, b bot.Bot, trigger store.Message, kind, text string, hop int) {
	if strings.TrimSpace(text) == "" {
		text = "(no answer)"
	}
	if _, err := e.insert(st, c.ID, b.Handle(), kind, text, &trigger, hop, nil); err != nil {
		e.logf("post reply from %s: %v", b.Handle(), err)
	}
}

func (e *Engine) handleDecision(b bot.Bot, c store.Chat, st *store.Store, trigger store.Message, j job, d gate.Decision, body string) {
	switch d.Decision {
	case gate.Refuse:
		e.reply(st, c, b, trigger, store.MsgRefusal, refusalText(body, d.Reason, e.suggest(b, c.Scope, j.Text)), j.Hop)
		if j.Routed {
			e.saveHint(c.Scope, fmt.Sprintf("%s refused messages like %q (%s).", b.Handle(), clip(j.Text, 160), d.Reason))
		}
	case gate.Delegate:
		if strings.TrimSpace(body) != "" {
			e.reply(st, c, b, trigger, store.MsgReply, body, j.Hop)
		}
		e.forward(b, c, st, trigger, j, d)
	default:
		e.reply(st, c, b, trigger, store.MsgReply, body, j.Hop)
	}
}

func refusalText(body, reason, suggestion string) string {
	text := strings.TrimSpace(body)
	if text == "" {
		text = "This is outside my charter."
	}
	if reason != "" && !strings.Contains(text, reason) {
		text += "\nReason: " + reason
	}
	if suggestion != "" {
		text += "\nTry " + suggestion + "."
	}
	return text
}

// forward passes a delegated request to its target: in the same chat when
// the target may access it, otherwise through a DM between the two bots.
func (e *Engine) forward(b bot.Bot, c store.Chat, st *store.Store, trigger store.Message, j job, d gate.Decision) {
	target, err := e.ResolveBot(d.Target, c.Scope)
	if err != nil {
		e.system(st, c.ID, &trigger, "%s delegated to %s, but: %v", b.Handle(), d.Target, err)
		return
	}
	hop := j.Hop + 1
	if hop > e.cfg.Limits.MaxHops {
		e.system(st, c.ID, &trigger, "Delegation stopped: hop limit (%d) reached.", e.cfg.Limits.MaxHops)
		return
	}
	if target.Handle() == b.Handle() || target.IsModerator() || !policy.CanDM(actorOf(b), actorOf(target)) {
		e.system(st, c.ID, &trigger, "%s may not delegate to %s.", b.Handle(), target.Handle())
		return
	}
	if c.Kind == store.KindGroup && policy.CanAccessChat(actorOf(target), c.Scope) {
		e.system(st, c.ID, &trigger, "%s delegated to %s (%s)", b.Handle(), target.Handle(), d.Reason)
		e.enqueue(job{Bot: target.Handle(), Chat: c.ID, MessageID: trigger.ID, Text: j.Text, Hop: hop, Root: j.Root})
		return
	}
	dm, dst, err := e.ensureDM(b.Handle(), target.Handle(), actorOf(b), actorOf(target))
	if err != nil {
		e.system(st, c.ID, &trigger, "delegation failed: %v", err)
		return
	}
	msg, err := e.insert(dst, dm.ID, b.Handle(), store.MsgCommand, j.Text, nil, hop, nil)
	if err != nil {
		e.system(st, c.ID, &trigger, "delegation failed: %v", err)
		return
	}
	e.system(st, c.ID, &trigger, "%s delegated to %s via %s (%s)", b.Handle(), target.Handle(), dm.ID, d.Reason)
	e.enqueue(job{Bot: target.Handle(), Chat: dm.ID, MessageID: msg.ID, Text: j.Text, Hop: hop, Root: j.Root})
}

// suggest names up to two other active bots whose topics appear in text.
func (e *Engine) suggest(b bot.Bot, scope, text string) string {
	bots, err := e.VisibleBots(scope)
	if err != nil {
		return ""
	}
	lower := strings.ToLower(text)
	var picks []string
	for _, o := range bots {
		if o.Handle() == b.Handle() || o.IsModerator() || o.Status != bot.StatusActive {
			continue
		}
		if slices.ContainsFunc(o.Topics, func(t string) bool { return t != "" && strings.Contains(lower, strings.ToLower(t)) }) {
			picks = append(picks, o.Handle())
		}
		if len(picks) == 2 {
			break
		}
	}
	return strings.Join(picks, " or ")
}

var errNoTurn = errors.New("unknown or expired turn token")
