package client_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/client"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/runtime"
)

func startDaemon(t *testing.T) (*client.Client, string) {
	t.Helper()
	short, err := os.MkdirTemp("", "rd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	home := filepath.Join(short, "h")
	t.Setenv(config.EnvHome, home)
	e, err := engine.New(engine.Options{Config: config.Default(), Global: config.Scope{Name: "global", Root: home},
		Runners: func(bot.Bot) (runtime.Runner, error) { return echoRunner{}, nil }, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(short, "r.sock")
	ln, err := daemon.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = daemon.Serve(ctx, ln, e); close(done) }()
	t.Cleanup(func() { cancel(); <-done; _ = e.Close() })
	c := client.New(socket)
	if err := c.WaitHealthy(context.Background(), 3*time.Second); err != nil {
		t.Fatal(err)
	}
	return c, short
}

func TestManagementAPI(t *testing.T) {
	c, short := startDaemon(t)
	ctx := context.Background()
	proj := filepath.Join(short, "shop")
	_ = os.MkdirAll(filepath.Join(proj, ".git"), 0o755)

	info, err := c.InspectFolder(ctx, proj)
	if err != nil || !info.IsGit || info.Registered {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	if _, err := c.SuggestFolders(ctx); err != nil {
		t.Fatal(err)
	}
	tpl, err := c.Templates(ctx)
	if err != nil || len(tpl) != 3 {
		t.Fatalf("templates: %+v %v", tpl, err)
	}
	res, err := c.Onboard(ctx, engine.OnboardInput{Dir: proj, Starters: []string{"docs"}, ChatName: "dev"})
	if err != nil || res.Project != "shop" || res.Chat.ID != "shop/dev" {
		t.Fatalf("onboard: %+v %v", res, err)
	}
	other := filepath.Join(short, "web")
	_ = os.MkdirAll(other, 0o755)
	if p, err := c.RegisterProject(ctx, daemon.ProjectAdd{Dir: other, Slug: "site"}); err != nil || p.Slug != "site" {
		t.Fatalf("register: %+v %v", p, err)
	}
	if ps, err := c.Projects(ctx); err != nil || len(ps) != 2 {
		t.Fatalf("projects: %+v %v", ps, err)
	}

	v, err := c.CreateBot(ctx, engine.BotInput{Scope: "global", Name: "ops", Charter: "Deploys."})
	if err != nil || v.Handle != "@global/ops" {
		t.Fatalf("create: %+v %v", v, err)
	}
	model := "opus"
	if u, err := c.UpdateBot(ctx, "@global/ops", engine.BotPatch{Model: &model}); err != nil || u.Model != "opus" {
		t.Fatalf("update: %+v %v", u, err)
	}
	if st, err := c.SetBotStatus(ctx, "@global/ops", "off"); err != nil || st.Bot.Status != "off" {
		t.Fatalf("status: %+v %v", st, err)
	}
	if got, err := c.GetBot(ctx, "ops", "global"); err != nil || got.Status != "off" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if bots, err := c.ListBots(ctx, "shop"); err != nil || len(bots) < 2 {
		t.Fatalf("list: %+v %v", bots, err)
	}
	en, err := c.AddKnowledge(ctx, "@global/ops", "Day", "Friday")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateKnowledge(ctx, "@global/ops", en.ID, "Day", "Thursday"); err != nil {
		t.Fatal(err)
	}
	if n, err := c.InheritKnowledge(ctx, "@shop/docs", "@global/ops"); err != nil || n != 1 {
		t.Fatalf("inherit: %d %v", n, err)
	}
	if ks, err := c.Knowledge(ctx, "@shop/docs"); err != nil || len(ks) != 1 || ks[0].Body != "Thursday" {
		t.Fatalf("knowledge: %+v %v", ks, err)
	}
	if err := c.ForgetKnowledge(ctx, "@global/ops", en.ID); err != nil {
		t.Fatal(err)
	}
	if ms, err := c.ChatMembers(ctx, "shop/dev"); err != nil || len(ms) != 1 {
		t.Fatalf("members: %+v %v", ms, err)
	}
	if _, err := c.Skills(ctx, "shop"); err != nil {
		t.Fatal(err)
	}
	if ps, err := c.Providers(ctx); err != nil || ps[0].Name != "claude-cli" {
		t.Fatalf("providers: %+v %v", ps, err)
	}
	if s, err := c.Settings(ctx); err != nil || s.MaxTurns == 0 {
		t.Fatalf("settings: %+v %v", s, err)
	}
	if _, err := c.CreateBot(ctx, engine.BotInput{Scope: "global", Name: "Bad Name", Charter: "x"}); err == nil {
		t.Fatal("invalid bot accepted")
	}
}

func TestWebListenerSecurity(t *testing.T) {
	c, _ := startDaemon(t)
	ctx := context.Background()
	raw, err := c.StartUI(ctx)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := c.StartUI(ctx)
	if again != raw {
		t.Fatal("second start should reuse the listener")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(u.Fragment, "token=") {
		t.Fatalf("ui url: %s %v", raw, err)
	}
	token := strings.TrimPrefix(u.Fragment, "token=")
	base := "http://" + u.Host

	do := func(method, path, auth, host, origin string) (int, string) {
		req, _ := http.NewRequest(method, base+path, nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		if host != "" {
			req.Host = host
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	if code, body := do("GET", "/", "", "", ""); code != 200 || !strings.Contains(body, "Realm") {
		t.Fatalf("index: %d %s", code, body)
	}
	if code, _ := do("GET", "/bots/some/route", "", "", ""); code != 200 {
		t.Fatalf("spa fallback: %d", code)
	}
	if code, _ := do("GET", "/api/v1/bots?scope=global", token, "", ""); code != 200 {
		t.Fatalf("authorized api: %d", code)
	}
	if code, _ := do("GET", "/api/v1/bots", "", "", ""); code != 401 {
		t.Fatalf("missing token: %d", code)
	}
	if code, _ := do("GET", "/api/v1/bots", "wrong", "", ""); code != 401 {
		t.Fatalf("wrong token: %d", code)
	}
	if code, _ := do("GET", "/api/v1/bots", token, "evil.example:"+u.Port(), ""); code != 403 {
		t.Fatalf("bad host (DNS rebinding): %d", code)
	}
	if code, _ := do("POST", "/api/v1/bots", token, "", "http://evil.example"); code != 403 {
		t.Fatalf("bad origin (CSRF): %d", code)
	}
	if code, _ := do("GET", "/api/v1/bots?scope=global", token, "", "http://localhost:"+u.Port()); code != 200 {
		t.Fatalf("same origin: %d", code)
	}
	for _, path := range []string{"/api/v1/tools/recall", "/api/v1/shutdown", "/api/v1/ui"} {
		if code, _ := do("POST", path, token, "", ""); code != 404 && code != 405 {
			t.Fatalf("%s must not be exposed on the web listener: %d", path, code)
		}
	}
	if code, _ := do("GET", "/api/v1/stream?chat=global/none&token="+token, "", "", ""); code != 404 {
		t.Fatalf("stream with query token: %d", code)
	}
}
