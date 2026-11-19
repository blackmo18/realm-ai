// Package daemon serves the engine over HTTP on a unix socket, and serves
// the web UI on a local TCP port when asked.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/store"
)

const maxBody = 1 << 20

// Envelope is every JSON response.
type Envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// Request bodies.
type (
	ChatCreate struct {
		Scope   string   `json:"scope"`
		Name    string   `json:"name"`
		Routing string   `json:"routing"`
		Members []string `json:"members"`
	}
	ChatMember struct {
		Chat string `json:"chat"`
		Bot  string `json:"bot"`
	}
	ChatRouting struct {
		Chat    string `json:"chat"`
		Routing string `json:"routing"`
	}
	Post struct {
		Chat string `json:"chat"`
		Text string `json:"text"`
	}
	DM struct {
		To    string `json:"to"`
		Scope string `json:"scope"`
		Text  string `json:"text"`
	}
	Pick struct {
		Chat string `json:"chat"`
		Ask  string `json:"ask"`
		Bot  string `json:"bot"`
	}
	Reroute struct {
		Chat    string `json:"chat"`
		Message string `json:"message"`
		Bot     string `json:"bot"`
	}
	BotChanged struct {
		Handle string `json:"handle"`
	}
	BotUpdate struct {
		Handle string          `json:"handle"`
		Patch  engine.BotPatch `json:"patch"`
	}
	BotStatus struct {
		Handle string `json:"handle"`
		Action string `json:"action"`
	}
	BotInherit struct {
		Handle string `json:"handle"`
		From   string `json:"from"`
	}
	KnowledgeAdd struct {
		Handle string `json:"handle"`
		ID     string `json:"id"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	}
	ProjectAdd struct {
		Dir      string `json:"dir"`
		Slug     string `json:"slug"`
		IgnoreDB bool   `json:"ignore_db"`
	}
	// StatusResult is the reply to a lifecycle change.
	StatusResult struct {
		Bot        engine.BotView `json:"bot"`
		Dispatched int            `json:"dispatched"`
	}
	// UIResult is the reply to a web UI start request.
	UIResult struct {
		URL string `json:"url"`
	}
)

// routes registers the API shared by the unix socket and the web UI.
func routes(mux *http.ServeMux, e *engine.Engine) {
	q := func(r *http.Request, k string) string { return r.URL.Query().Get(k) }
	get := func(path string, fn func(*http.Request) (any, error)) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { reply(w)(fn(r)) })
	}
	get("/v1/health", func(*http.Request) (any, error) { return map[string]any{"pid": os.Getpid()}, nil })
	get("/v1/chats", func(r *http.Request) (any, error) { return e.ListChats(q(r, "scope")) })
	get("/v1/chat", func(r *http.Request) (any, error) { return e.GetChat(q(r, "id")) })
	get("/v1/chats/members", func(r *http.Request) (any, error) { return e.ChatMembers(q(r, "chat")) })
	get("/v1/messages", func(r *http.Request) (any, error) {
		limit, _ := strconv.Atoi(q(r, "limit"))
		return e.Messages(q(r, "chat"), limit)
	})
	get("/v1/bots", func(r *http.Request) (any, error) { return e.ListBots(q(r, "scope")) })
	get("/v1/bot", func(r *http.Request) (any, error) { return e.GetBot(q(r, "handle"), q(r, "scope")) })
	get("/v1/knowledge", func(r *http.Request) (any, error) { return e.Knowledge(q(r, "handle")) })
	get("/v1/projects", func(*http.Request) (any, error) { return e.Projects() })
	get("/v1/projects/inspect", func(r *http.Request) (any, error) { return e.InspectFolder(q(r, "path")) })
	get("/v1/projects/suggest", func(*http.Request) (any, error) { return e.SuggestFolders(8) })
	get("/v1/templates", func(*http.Request) (any, error) { return engine.Templates(), nil })
	get("/v1/skills", func(r *http.Request) (any, error) { return e.ListSkills(q(r, "scope")) })
	get("/v1/providers", func(*http.Request) (any, error) { return e.Providers(), nil })
	get("/v1/settings", func(*http.Request) (any, error) { return e.Settings(), nil })

	mux.HandleFunc("POST /v1/chats", handle(func(in ChatCreate) (any, error) {
		return e.CreateChat(in.Scope, in.Name, in.Routing, in.Members)
	}))
	mux.HandleFunc("POST /v1/chats/members", handle(func(in ChatMember) (any, error) { return e.AddMember(in.Chat, in.Bot) }))
	mux.HandleFunc("POST /v1/chats/routing", handle(func(in ChatRouting) (any, error) {
		return in.Routing, e.SetRouting(in.Chat, in.Routing)
	}))
	mux.HandleFunc("POST /v1/messages", handle(func(in Post) (any, error) { return e.PostUser(in.Chat, in.Text) }))
	mux.HandleFunc("POST /v1/dm", handle(func(in DM) (any, error) { return e.DMUser(in.To, in.Scope, in.Text) }))
	mux.HandleFunc("POST /v1/pick", handle(func(in Pick) (any, error) { return e.Pick(in.Chat, in.Ask, in.Bot) }))
	mux.HandleFunc("POST /v1/reroute", handle(func(in Reroute) (any, error) { return e.Reroute(in.Chat, in.Message, in.Bot) }))
	mux.HandleFunc("POST /v1/bots", handle(func(in engine.BotInput) (any, error) { return e.CreateBot(in) }))
	mux.HandleFunc("POST /v1/bots/update", handle(func(in BotUpdate) (any, error) { return e.UpdateBot(in.Handle, in.Patch) }))
	mux.HandleFunc("POST /v1/bots/status", handle(func(in BotStatus) (any, error) {
		v, n, err := e.SetBotStatus(in.Handle, in.Action)
		return StatusResult{Bot: v, Dispatched: n}, err
	}))
	mux.HandleFunc("POST /v1/bots/inherit", handle(func(in BotInherit) (any, error) { return e.InheritKnowledge(in.Handle, in.From) }))
	mux.HandleFunc("POST /v1/bots/changed", handle(func(in BotChanged) (any, error) { return e.BotChanged(in.Handle) }))
	mux.HandleFunc("POST /v1/knowledge", handle(func(in KnowledgeAdd) (any, error) { return e.AddKnowledge(in.Handle, in.Title, in.Body) }))
	mux.HandleFunc("POST /v1/knowledge/update", handle(func(in KnowledgeAdd) (any, error) {
		return e.UpdateKnowledge(in.Handle, in.ID, in.Title, in.Body)
	}))
	mux.HandleFunc("POST /v1/knowledge/forget", handle(func(in KnowledgeAdd) (any, error) { return in.ID, e.ForgetKnowledge(in.Handle, in.ID) }))
	mux.HandleFunc("POST /v1/projects", handle(func(in ProjectAdd) (any, error) {
		sc, err := e.RegisterProjectAs(in.Dir, in.Slug, in.IgnoreDB)
		return store.Project{Slug: sc.Name, Root: sc.ProjectDir()}, err
	}))
	mux.HandleFunc("POST /v1/onboard", handle(func(in engine.OnboardInput) (any, error) { return e.Onboard(in) }))
	mux.HandleFunc("GET /v1/stream", func(w http.ResponseWriter, r *http.Request) { stream(w, r, e) })
}

// Handler returns the full daemon API for the unix socket. It adds the
// bot tool endpoint, shutdown and web UI start, which the web listener
// never exposes.
func Handler(e *engine.Engine, shutdown func(), startUI func() (string, error)) http.Handler {
	mux := http.NewServeMux()
	routes(mux, e)
	mux.HandleFunc("POST /v1/shutdown", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{"stopping": true})
		if shutdown != nil {
			go shutdown()
		}
	})
	mux.HandleFunc("POST /v1/ui", func(w http.ResponseWriter, r *http.Request) {
		if startUI == nil {
			fail(w, http.StatusNotImplemented, errors.New("web UI not available"))
			return
		}
		url, err := startUI()
		reply(w)(UIResult{URL: url}, err)
	})
	mux.HandleFunc("POST /v1/tools/{name}", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		reply(w)(e.CallTool(r.Context(), token, r.PathValue("name"), raw))
	})
	return mux
}

func handle[T any](fn func(T) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in T
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&in); err != nil {
			fail(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
			return
		}
		reply(w)(fn(in))
	}
}

func reply(w http.ResponseWriter) func(any, error) {
	return func(v any, err error) {
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			}
			fail(w, status, err)
			return
		}
		ok(w, v)
	}
}

func ok(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	write(w, http.StatusOK, Envelope{OK: true, Data: data})
}

func fail(w http.ResponseWriter, status int, err error) {
	write(w, status, Envelope{OK: false, Error: err.Error()})
}

func write(w http.ResponseWriter, status int, env Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

func stream(w http.ResponseWriter, r *http.Request, e *engine.Engine) {
	chat := r.URL.Query().Get("chat")
	if _, err := e.GetChat(chat); err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	flusher, isFlusher := w.(http.Flusher)
	if !isFlusher {
		fail(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	msgs, cancel := e.Hub().Subscribe(chat)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case m, open := <-msgs:
			if !open {
				return
			}
			data, _ := json.Marshal(m)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// Listen opens the unix socket, replacing a stale one. It fails if another
// daemon is already serving on it.
func Listen(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return nil, err
	}
	if conn, err := net.DialTimeout("unix", socket, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("a realm daemon is already running on %s", socket)
	}
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socket, err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// Serve runs the API on ln until ctx is cancelled or shutdown is requested.
// The web UI listener, once started, stops with it.
func Serve(ctx context.Context, ln net.Listener, e *engine.Engine) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	web := &Web{e: e}
	defer web.Close()
	srv := &http.Server{Handler: Handler(e, cancel, web.Start), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = srv.Shutdown(shutdownCtx)
	return nil
}
