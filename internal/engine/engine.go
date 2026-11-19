// Package engine is the Realm core: scopes, bots, chats, message routing,
// bot turns and realm tool calls. The daemon serves it over a unix socket.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/policy"
	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/runtime/apiprovider"
	"github.com/blackmo18/realm-ai/internal/runtime/claudecli"
	"github.com/blackmo18/realm-ai/internal/store"
)

// RunnerFactory returns the runner for a bot.
type RunnerFactory func(b bot.Bot) (runtime.Runner, error)

// Options configure an Engine.
type Options struct {
	Config config.Config
	Global config.Scope
	// SelfBin is the realm executable used to launch `realm mcp` for
	// claude-cli turns. Empty disables realm tools for claude-cli bots.
	SelfBin string
	// Socket is the daemon socket the MCP server connects back to.
	Socket string
	// Runners overrides runner selection (tests).
	Runners RunnerFactory
	// Logf logs engine events (default log.Printf).
	Logf func(format string, args ...any)
}

// Engine is safe for concurrent use.
type Engine struct {
	cfg     config.Config
	global  config.Scope
	selfBin string
	socket  string
	runners RunnerFactory
	logf    func(string, ...any)
	hub     *Hub

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	sem    chan struct{}

	mu        sync.Mutex
	closed    bool
	stores    map[string]*store.Store
	botLocks  map[string]chan struct{}
	turns     map[string]turnCtx
	rootTurns map[string]int
}

// New creates an engine and opens the global store.
func New(opts Options) (*Engine, error) {
	if opts.Global.Root == "" {
		return nil, errors.New("global scope root is required")
	}
	if err := os.MkdirAll(opts.Global.Root, 0o700); err != nil {
		return nil, fmt.Errorf("create global home: %w", err)
	}
	_ = os.Chmod(opts.Global.Root, 0o700)
	maxConc := opts.Config.Limits.MaxConcurrent
	if maxConc <= 0 {
		maxConc = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		cfg: opts.Config, global: opts.Global, selfBin: opts.SelfBin, socket: opts.Socket,
		runners: opts.Runners, logf: opts.Logf, hub: NewHub(),
		ctx: ctx, cancel: cancel, sem: make(chan struct{}, maxConc),
		stores: map[string]*store.Store{}, botLocks: map[string]chan struct{}{}, turns: map[string]turnCtx{},
		rootTurns: map[string]int{},
	}
	if e.logf == nil {
		e.logf = log.Printf
	}
	if e.runners == nil {
		e.runners = e.defaultRunner
	}
	if _, err := e.store(config.GlobalScope); err != nil {
		cancel()
		return nil, err
	}
	return e, nil
}

// Hub returns the message hub for live subscriptions.
func (e *Engine) Hub() *Hub { return e.hub }

// Wait blocks until all background work has finished.
func (e *Engine) Wait() { e.wg.Wait() }

// Close stops background work and closes databases.
func (e *Engine) Close() error {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	e.cancel()
	e.wg.Wait()
	e.mu.Lock()
	defer e.mu.Unlock()
	var errs []error
	for _, s := range e.stores {
		errs = append(errs, s.Close())
	}
	e.stores = map[string]*store.Store{}
	return errors.Join(errs...)
}

// track registers background work; it returns false once Close has begun.
func (e *Engine) track() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return false
	}
	e.wg.Add(1)
	return true
}

// spend counts one bot turn against the message that started the chain and
// reports whether the chain is still within its budget.
func (e *Engine) spend(root string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rootTurns[root]++
	return e.rootTurns[root] <= maxTurnsPerMessage
}

func (e *Engine) defaultRunner(b bot.Bot) (runtime.Runner, error) {
	if b.Provider == bot.ProviderClaudeCLI {
		return claudecli.Runner{Bin: e.cfg.ClaudeBin}, nil
	}
	p, ok := e.cfg.Providers[b.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (configure it in %s)", b.Provider, config.Path(e.global.Root))
	}
	return apiprovider.New(p)
}

func (e *Engine) turnTimeout() time.Duration {
	if s := e.cfg.Limits.TurnTimeoutSec; s > 0 {
		return time.Duration(s) * time.Second
	}
	return 10 * time.Minute
}

// --- scopes ---

// ScopeByName resolves a scope name to its paths.
func (e *Engine) ScopeByName(name string) (config.Scope, error) {
	if name == config.GlobalScope || name == "" {
		return e.global, nil
	}
	gs, err := e.store(config.GlobalScope)
	if err != nil {
		return config.Scope{}, err
	}
	p, err := gs.ProjectBySlug(name)
	if err != nil {
		return config.Scope{}, err
	}
	return config.Scope{Name: p.Slug, Root: filepath.Join(p.Root, config.DirName)}, nil
}

func (e *Engine) store(scope string) (*store.Store, error) {
	if scope == "" {
		scope = config.GlobalScope
	}
	e.mu.Lock()
	s, ok := e.stores[scope]
	e.mu.Unlock()
	if ok {
		return s, nil
	}
	var sc config.Scope
	if scope == config.GlobalScope {
		sc = e.global
	} else {
		var err error
		if sc, err = e.ScopeByName(scope); err != nil {
			return nil, err
		}
	}
	opened, err := store.Open(sc.DBPath())
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, ok := e.stores[scope]; ok {
		_ = opened.Close()
		return existing, nil
	}
	e.stores[scope] = opened
	return opened, nil
}

// RegisterProject initializes <dir>/.realm and registers the project.
func (e *Engine) RegisterProject(dir string) (config.Scope, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return config.Scope{}, err
	}
	if abs == filepath.Dir(e.global.Root) && filepath.Base(e.global.Root) == config.DirName {
		return config.Scope{}, errors.New("cannot register your home folder as a project")
	}
	if err := os.MkdirAll(filepath.Join(abs, config.DirName, "bots"), 0o755); err != nil {
		return config.Scope{}, fmt.Errorf("create project realm folder: %w", err)
	}
	gs, err := e.store(config.GlobalScope)
	if err != nil {
		return config.Scope{}, err
	}
	p, err := gs.RegisterProject(config.Slugify(filepath.Base(abs)), abs)
	if err != nil {
		return config.Scope{}, err
	}
	return config.Scope{Name: p.Slug, Root: filepath.Join(abs, config.DirName)}, nil
}

// ProjectForDir returns the registered project scope enclosing dir.
func (e *Engine) ProjectForDir(dir string) (config.Scope, error) {
	root, err := config.FindProjectRoot(dir)
	if err != nil {
		return config.Scope{}, err
	}
	gs, err := e.store(config.GlobalScope)
	if err != nil {
		return config.Scope{}, err
	}
	p, err := gs.ProjectByRoot(root)
	if err != nil {
		return config.Scope{}, fmt.Errorf("%s has a .realm folder but is not registered; run `realm init` there: %w", root, err)
	}
	return config.Scope{Name: p.Slug, Root: filepath.Join(root, config.DirName)}, nil
}

// Projects lists registered projects.
func (e *Engine) Projects() ([]store.Project, error) {
	gs, err := e.store(config.GlobalScope)
	if err != nil {
		return nil, err
	}
	return gs.Projects()
}

// --- bots ---

// Repo returns the bot repository of a scope.
func (e *Engine) Repo(scope string) (*bot.Repo, error) {
	sc, err := e.ScopeByName(scope)
	if err != nil {
		return nil, err
	}
	return bot.NewRepo(sc), nil
}

// ResolveBot finds a bot by handle. Unqualified handles resolve in ctxScope
// first, then global.
func (e *Engine) ResolveBot(handle, ctxScope string) (bot.Bot, error) {
	addr, err := bot.ParseAddress(handle)
	if err != nil {
		return bot.Bot{}, err
	}
	if addr.Qualified() {
		repo, err := e.Repo(addr.Scope)
		if err != nil {
			return bot.Bot{}, err
		}
		return repo.Get(addr.Name)
	}
	if ctxScope != "" && ctxScope != config.GlobalScope {
		repo, err := e.Repo(ctxScope)
		if err != nil {
			return bot.Bot{}, err
		}
		if b, err := repo.Get(addr.Name); err == nil {
			return b, nil
		}
	}
	repo, err := e.Repo(config.GlobalScope)
	if err != nil {
		return bot.Bot{}, err
	}
	b, err := repo.Get(addr.Name)
	if errors.Is(err, bot.ErrNotFound) {
		return bot.Bot{}, fmt.Errorf("%w: %s", bot.ErrNotFound, handle)
	}
	return b, err
}

// VisibleBots lists bots an actor in scope can reach: global bots plus the
// bots of its own project (a global actor sees every project's bots).
func (e *Engine) VisibleBots(scope string) ([]bot.Bot, error) {
	scopes := []string{config.GlobalScope}
	if scope == config.GlobalScope {
		projects, err := e.Projects()
		if err != nil {
			return nil, err
		}
		for _, p := range projects {
			scopes = append(scopes, p.Slug)
		}
	} else if scope != "" {
		scopes = append(scopes, scope)
	}
	var out []bot.Bot
	for _, s := range scopes {
		repo, err := e.Repo(s)
		if err != nil {
			return nil, err
		}
		bots, err := repo.List()
		if err != nil {
			return nil, err
		}
		out = append(out, bots...)
	}
	return out, nil
}

func actorOf(b bot.Bot) policy.Actor { return policy.Bot(b.Scope) }

// ensureModerator creates the scope's moderator bot if missing.
func (e *Engine) ensureModerator(scope string) (bot.Bot, error) {
	repo, err := e.Repo(scope)
	if err != nil {
		return bot.Bot{}, err
	}
	if b, err := repo.Get(bot.ModeratorName); err == nil {
		return b, nil
	}
	return repo.Create(bot.Bot{
		Name: bot.ModeratorName, Role: bot.RoleModerator, Provider: bot.ProviderClaudeCLI,
		Model: e.cfg.ModeratorModel, Effort: "low",
		Charter: "Route each un-mentioned chat message to the member bot that owns it. Ask the user when ownership is ambiguous. Never answer requests yourself.",
	})
}
