package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/runtime"
	"github.com/blackmo18/realm-ai/internal/store"
)

type call struct {
	Bot  string
	Turn runtime.Turn
}

// fakeModels records every model call and answers per bot handle.
type fakeModels struct {
	mu      sync.Mutex
	calls   []call
	answers map[string]func(runtime.Turn) (string, error)
}

func (f *fakeModels) factory(b bot.Bot) (runtime.Runner, error) {
	return fakeRunner{f: f, handle: b.Handle()}, nil
}

func (f *fakeModels) answer(handle string, fn func(runtime.Turn) (string, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[handle] = fn
}

func (f *fakeModels) say(handle, text string) {
	f.answer(handle, func(runtime.Turn) (string, error) { return text, nil })
}

func (f *fakeModels) callsFor(handle string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Bot == handle {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeModels) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeRunner struct {
	f      *fakeModels
	handle string
}

func (r fakeRunner) Run(_ context.Context, t runtime.Turn) (runtime.Result, error) {
	r.f.mu.Lock()
	r.f.calls = append(r.f.calls, call{Bot: r.handle, Turn: t})
	fn := r.f.answers[r.handle]
	r.f.mu.Unlock()
	if fn == nil {
		return runtime.Result{Text: "{\"decision\":\"act\"}\nok from " + r.handle, SessionID: "sess-" + r.handle}, nil
	}
	text, err := fn(t)
	return runtime.Result{Text: text, SessionID: "sess-" + r.handle}, err
}

type fixture struct {
	e       *Engine
	models  *fakeModels
	project config.Scope
	projDir string
}

func setup(t *testing.T) fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, filepath.Join(home, ".realm"))
	models := &fakeModels{answers: map[string]func(runtime.Turn) (string, error){}}
	cfg := config.Default()
	cfg.Limits.MaxHops = 2
	e, err := New(Options{Config: cfg, Global: config.Scope{Name: config.GlobalScope, Root: filepath.Join(home, ".realm")},
		Runners: models.factory, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	projDir := filepath.Join(home, "code", "app")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	proj, err := e.RegisterProject(projDir)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{e: e, models: models, project: proj, projDir: projDir}
	f.bot(t, "app", bot.Bot{Name: "reviewer", Charter: "Reviews Go code.", Topics: []string{"go"}})
	f.bot(t, "app", bot.Bot{Name: "db", Charter: "Owns schema and migrations.", Topics: []string{"schema"}})
	f.bot(t, config.GlobalScope, bot.Bot{Name: "ops", Charter: "Deploys services.", Topics: []string{"deploy"}})
	return f
}

func (f fixture) bot(t *testing.T, scope string, b bot.Bot) bot.Bot {
	t.Helper()
	repo, err := f.e.Repo(scope)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repo.Create(b)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func (f fixture) chat(t *testing.T, scope, name, routing string, members ...string) store.Chat {
	t.Helper()
	c, err := f.e.CreateChat(scope, name, routing, members)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f fixture) post(t *testing.T, chatID, text string) store.Message {
	t.Helper()
	m, err := f.e.PostUser(chatID, text)
	if err != nil {
		t.Fatal(err)
	}
	f.e.Wait()
	return m
}

func (f fixture) messages(t *testing.T, chatID string) []store.Message {
	t.Helper()
	msgs, err := f.e.Messages(chatID, 100)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

func findMsg(msgs []store.Message, from, kind, contains string) (store.Message, bool) {
	for _, m := range msgs {
		if (from == "" || m.From == from) && (kind == "" || m.Kind == kind) && strings.Contains(m.Text, contains) {
			return m, true
		}
	}
	return store.Message{}, false
}

func (f fixture) setStatus(t *testing.T, scope, name, action string) {
	t.Helper()
	repo, _ := f.e.Repo(scope)
	if _, err := repo.Apply(name, action); err != nil {
		t.Fatal(err)
	}
}
