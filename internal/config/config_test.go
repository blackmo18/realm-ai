package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.MaxHops != 4 || cfg.ModeratorModel != "haiku" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadMergesFileOverDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "claude_bin: /opt/claude\nlimits:\n  max_hops: 2\nproviders:\n  groq:\n    type: openai-compat\n    base_url: https://api.groq.com/openai/v1\n    api_key_env: GROQ_API_KEY\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClaudeBin != "/opt/claude" || cfg.Limits.MaxHops != 2 || cfg.Limits.MaxConcurrent != 3 {
		t.Fatalf("merge failed: %+v", cfg)
	}
	if _, ok := cfg.Providers["groq"]; !ok {
		t.Fatal("groq provider missing")
	}
	if _, ok := cfg.Providers["anthropic"]; !ok {
		t.Fatal("default anthropic provider dropped")
	}
}

func TestLoadRejectsBadProvider(t *testing.T) {
	cases := map[string]string{
		"unknown type": "providers:\n  x:\n    type: magic\n    base_url: http://x\n",
		"no base url":  "providers:\n  x:\n    type: anthropic\n",
		"reserved":     "providers:\n  claude-cli:\n    type: anthropic\n    base_url: http://x\n",
		"bad yaml":     "providers: [",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAPIKeyFromEnv(t *testing.T) {
	p := ProviderConfig{APIKeyEnv: "REALM_TEST_KEY"}
	t.Setenv("REALM_TEST_KEY", "")
	if _, err := p.APIKey(); err == nil {
		t.Fatal("expected error for unset key")
	}
	t.Setenv("REALM_TEST_KEY", "sk-test")
	if k, err := p.APIKey(); err != nil || k != "sk-test" {
		t.Fatalf("APIKey() = %q, %v", k, err)
	}
	if k, err := (ProviderConfig{}).APIKey(); err != nil || k != "" {
		t.Fatalf("keyless provider: %q, %v", k, err)
	}
}

func TestWriteDefaultDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("claude_bin: mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.ClaudeBin != "mine" {
		t.Fatalf("overwritten: %+v %v", cfg, err)
	}
}

func TestFindProjectRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, filepath.Join(home, ".realm"))
	if err := os.MkdirAll(filepath.Join(home, ".realm"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(home, "code", "app")
	deep := filepath.Join(proj, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := FindProjectRoot(deep); err != ErrNoProject {
		t.Fatalf("want ErrNoProject before init, got %v", err)
	}
	if err := os.MkdirAll(filepath.Join(proj, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindProjectRoot(deep)
	if err != nil || got != proj {
		t.Fatalf("FindProjectRoot = %q, %v; want %q", got, err, proj)
	}
}

func TestFindProjectRootSkipsGlobalHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, filepath.Join(home, DirName))
	if err := os.MkdirAll(filepath.Join(home, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := FindProjectRoot(home); err != ErrNoProject {
		t.Fatalf("global home treated as project: %v", err)
	}
}

func TestScopePaths(t *testing.T) {
	g := Scope{Name: GlobalScope, Root: "/h/.realm"}
	if !g.IsGlobal() || g.ProjectDir() != "" || g.BotsDir() != "/h/.realm/bots" || g.DBPath() != "/h/.realm/realm.db" {
		t.Fatalf("global scope paths wrong: %+v", g)
	}
	p := Scope{Name: "app", Root: "/code/app/.realm"}
	if p.IsGlobal() || p.ProjectDir() != "/code/app" {
		t.Fatalf("project scope paths wrong: %+v", p)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Realm AI":  "realm-ai",
		"my_app.v2": "my-app-v2",
		"global":    "project-global",
		"9lives":    "p-9lives",
		"___":       "project",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGlobal(t *testing.T) {
	t.Setenv(EnvHome, "/tmp/realm-x")
	s, err := Global()
	if err != nil || s.Root != "/tmp/realm-x" || !s.IsGlobal() {
		t.Fatalf("Global() = %+v, %v", s, err)
	}
}
