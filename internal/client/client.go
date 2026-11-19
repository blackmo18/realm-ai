// Package client talks to the realm daemon over its unix socket.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/knowledge"
	"github.com/blackmo18/realm-ai/internal/store"
)

// ErrNotRunning means no daemon answered on the socket.
var ErrNotRunning = errors.New("realm daemon is not running")

// Client is a daemon API client.
type Client struct {
	socket string
	http   *http.Client
}

// New returns a client for the daemon socket.
func New(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{socket: socket, http: &http.Client{Transport: tr}}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://realm"+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.send(req, out)
}

func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return ErrNotRunning
		}
		return err
	}
	defer resp.Body.Close()
	var env daemon.Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("bad daemon response (%s): %w", resp.Status, err)
	}
	if !env.OK {
		return errors.New(env.Error)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// Health checks the daemon.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/v1/health", nil, nil)
}

// Shutdown asks the daemon to stop.
func (c *Client) Shutdown(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/shutdown", nil, nil)
}

// CreateChat creates a group chat.
func (c *Client) CreateChat(ctx context.Context, in daemon.ChatCreate) (store.Chat, error) {
	var out store.Chat
	return out, c.do(ctx, http.MethodPost, "/v1/chats", in, &out)
}

// ListChats lists chats in a scope.
func (c *Client) ListChats(ctx context.Context, scope string) ([]store.Chat, error) {
	var out []store.Chat
	return out, c.do(ctx, http.MethodGet, "/v1/chats?scope="+url.QueryEscape(scope), nil, &out)
}

// GetChat loads a chat.
func (c *Client) GetChat(ctx context.Context, id string) (store.Chat, error) {
	var out store.Chat
	return out, c.do(ctx, http.MethodGet, "/v1/chat?id="+url.QueryEscape(id), nil, &out)
}

// AddMember adds a bot to a chat.
func (c *Client) AddMember(ctx context.Context, chat, bot string) (string, error) {
	var out string
	return out, c.do(ctx, http.MethodPost, "/v1/chats/members", daemon.ChatMember{Chat: chat, Bot: bot}, &out)
}

// SetRouting changes chat routing.
func (c *Client) SetRouting(ctx context.Context, chat, routing string) error {
	return c.do(ctx, http.MethodPost, "/v1/chats/routing", daemon.ChatRouting{Chat: chat, Routing: routing}, nil)
}

// Messages returns recent messages.
func (c *Client) Messages(ctx context.Context, chat string, limit int) ([]store.Message, error) {
	var out []store.Message
	q := fmt.Sprintf("/v1/messages?chat=%s&limit=%d", url.QueryEscape(chat), limit)
	return out, c.do(ctx, http.MethodGet, q, nil, &out)
}

// Post sends a user message to a chat.
func (c *Client) Post(ctx context.Context, chat, text string) (store.Message, error) {
	var out store.Message
	return out, c.do(ctx, http.MethodPost, "/v1/messages", daemon.Post{Chat: chat, Text: text}, &out)
}

// DM sends a user direct message to a bot.
func (c *Client) DM(ctx context.Context, to, scope, text string) (store.Message, error) {
	var out store.Message
	return out, c.do(ctx, http.MethodPost, "/v1/dm", daemon.DM{To: to, Scope: scope, Text: text}, &out)
}

// Pick answers a moderator question.
func (c *Client) Pick(ctx context.Context, chat, ask, bot string) (string, error) {
	var out string
	return out, c.do(ctx, http.MethodPost, "/v1/pick", daemon.Pick{Chat: chat, Ask: ask, Bot: bot}, &out)
}

// Reroute sends a message to another bot.
func (c *Client) Reroute(ctx context.Context, chat, msg, bot string) (string, error) {
	var out string
	return out, c.do(ctx, http.MethodPost, "/v1/reroute", daemon.Reroute{Chat: chat, Message: msg, Bot: bot}, &out)
}

// BotChanged tells the daemon a bot's status changed.
func (c *Client) BotChanged(ctx context.Context, handle string) (int, error) {
	var out int
	return out, c.do(ctx, http.MethodPost, "/v1/bots/changed", daemon.BotChanged{Handle: handle}, &out)
}

// Tool runs a realm tool for the turn that owns token.
func (c *Client) Tool(ctx context.Context, token, name string, args json.RawMessage) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://realm/v1/tools/"+url.PathEscape(name), bytes.NewReader(args))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	var out string
	return out, c.send(req, &out)
}

// Stream subscribes to new messages of a chat. The channel closes when ctx
// ends or the daemon goes away.
func (c *Client) Stream(ctx context.Context, chat string) (<-chan store.Message, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://realm/v1/stream?chat="+url.QueryEscape(chat), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, ErrNotRunning
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var env daemon.Envelope
		_ = json.NewDecoder(resp.Body).Decode(&env)
		return nil, fmt.Errorf("stream %s: %s", chat, env.Error)
	}
	out := make(chan store.Message, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			data, isData := strings.CutPrefix(sc.Text(), "data: ")
			if !isData {
				continue
			}
			var m store.Message
			if json.Unmarshal([]byte(data), &m) != nil {
				continue
			}
			select {
			case out <- m:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// WaitHealthy polls until the daemon answers or timeout passes.
func (c *Client) WaitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := c.Health(ctx); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrNotRunning
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// ListBots lists bots visible from a scope.
func (c *Client) ListBots(ctx context.Context, scope string) ([]engine.BotView, error) {
	var out []engine.BotView
	return out, c.get(ctx, "/v1/bots", url.Values{"scope": {scope}}, &out)
}

// GetBot loads one bot.
func (c *Client) GetBot(ctx context.Context, handle, scope string) (engine.BotView, error) {
	var out engine.BotView
	return out, c.get(ctx, "/v1/bot", url.Values{"handle": {handle}, "scope": {scope}}, &out)
}

// CreateBot creates a bot.
func (c *Client) CreateBot(ctx context.Context, in engine.BotInput) (engine.BotView, error) {
	var out engine.BotView
	return out, c.do(ctx, http.MethodPost, "/v1/bots", in, &out)
}

// UpdateBot patches a bot.
func (c *Client) UpdateBot(ctx context.Context, handle string, p engine.BotPatch) (engine.BotView, error) {
	var out engine.BotView
	return out, c.do(ctx, http.MethodPost, "/v1/bots/update", daemon.BotUpdate{Handle: handle, Patch: p}, &out)
}

// SetBotStatus runs on/off/archive/restore.
func (c *Client) SetBotStatus(ctx context.Context, handle, action string) (daemon.StatusResult, error) {
	var out daemon.StatusResult
	return out, c.do(ctx, http.MethodPost, "/v1/bots/status", daemon.BotStatus{Handle: handle, Action: action}, &out)
}

// InheritKnowledge copies knowledge from one bot into another.
func (c *Client) InheritKnowledge(ctx context.Context, handle, from string) (int, error) {
	var out int
	return out, c.do(ctx, http.MethodPost, "/v1/bots/inherit", daemon.BotInherit{Handle: handle, From: from}, &out)
}

// Knowledge lists a bot's knowledge.
func (c *Client) Knowledge(ctx context.Context, handle string) ([]knowledge.Entry, error) {
	var out []knowledge.Entry
	return out, c.get(ctx, "/v1/knowledge", url.Values{"handle": {handle}}, &out)
}

// AddKnowledge adds a knowledge entry.
func (c *Client) AddKnowledge(ctx context.Context, handle, title, body string) (knowledge.Entry, error) {
	var out knowledge.Entry
	return out, c.do(ctx, http.MethodPost, "/v1/knowledge", daemon.KnowledgeAdd{Handle: handle, Title: title, Body: body}, &out)
}

// UpdateKnowledge rewrites a knowledge entry.
func (c *Client) UpdateKnowledge(ctx context.Context, handle, id, title, body string) (knowledge.Entry, error) {
	var out knowledge.Entry
	return out, c.do(ctx, http.MethodPost, "/v1/knowledge/update", daemon.KnowledgeAdd{Handle: handle, ID: id, Title: title, Body: body}, &out)
}

// ForgetKnowledge deletes a knowledge entry.
func (c *Client) ForgetKnowledge(ctx context.Context, handle, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/knowledge/forget", daemon.KnowledgeAdd{Handle: handle, ID: id}, nil)
}

// ChatMembers lists a chat's routable bots.
func (c *Client) ChatMembers(ctx context.Context, chat string) ([]engine.BotView, error) {
	var out []engine.BotView
	return out, c.get(ctx, "/v1/chats/members", url.Values{"chat": {chat}}, &out)
}

// Projects lists registered projects.
func (c *Client) Projects(ctx context.Context) ([]store.Project, error) {
	var out []store.Project
	return out, c.get(ctx, "/v1/projects", nil, &out)
}

// RegisterProject registers a folder.
func (c *Client) RegisterProject(ctx context.Context, in daemon.ProjectAdd) (store.Project, error) {
	var out store.Project
	return out, c.do(ctx, http.MethodPost, "/v1/projects", in, &out)
}

// InspectFolder reports what a folder holds.
func (c *Client) InspectFolder(ctx context.Context, path string) (engine.FolderInfo, error) {
	var out engine.FolderInfo
	return out, c.get(ctx, "/v1/projects/inspect", url.Values{"path": {path}}, &out)
}

// SuggestFolders lists unregistered git folders.
func (c *Client) SuggestFolders(ctx context.Context) ([]string, error) {
	var out []string
	return out, c.get(ctx, "/v1/projects/suggest", nil, &out)
}

// Onboard runs the add-project flow.
func (c *Client) Onboard(ctx context.Context, in engine.OnboardInput) (engine.OnboardResult, error) {
	var out engine.OnboardResult
	return out, c.do(ctx, http.MethodPost, "/v1/onboard", in, &out)
}

// Templates lists starter bots.
func (c *Client) Templates(ctx context.Context) ([]engine.Template, error) {
	var out []engine.Template
	return out, c.get(ctx, "/v1/templates", nil, &out)
}

// Skills lists skills visible at a scope.
func (c *Client) Skills(ctx context.Context, scope string) ([]engine.Skill, error) {
	var out []engine.Skill
	return out, c.get(ctx, "/v1/skills", url.Values{"scope": {scope}}, &out)
}

// Providers lists runtimes and readiness.
func (c *Client) Providers(ctx context.Context) ([]engine.ProviderInfo, error) {
	var out []engine.ProviderInfo
	return out, c.get(ctx, "/v1/providers", nil, &out)
}

// Settings returns the effective configuration.
func (c *Client) Settings(ctx context.Context) (engine.Settings, error) {
	var out engine.Settings
	return out, c.get(ctx, "/v1/settings", nil, &out)
}

// StartUI starts the daemon's web UI listener and returns its URL.
func (c *Client) StartUI(ctx context.Context) (string, error) {
	var out daemon.UIResult
	return out.URL, c.do(ctx, http.MethodPost, "/v1/ui", nil, &out)
}
