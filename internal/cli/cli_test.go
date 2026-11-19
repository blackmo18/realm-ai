package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blackmo18/realm-ai/internal/config"
)

func run(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(&out, &errOut, strings.NewReader(""))
	root.SetArgs(args)
	t.Chdir(dir)
	err := root.Execute()
	return out.String() + errOut.String(), err
}

func must(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := run(t, dir, args...)
	if err != nil {
		t.Fatalf("realm %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func TestBotLifecycleCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, filepath.Join(home, ".realm"))
	proj := filepath.Join(home, "app")
	_ = os.MkdirAll(proj, 0o755)

	must(t, home, "init", "--global")
	if out := must(t, proj, "init"); !strings.Contains(out, `project "app" ready`) {
		t.Fatalf("init: %s", out)
	}
	if out := must(t, proj, "bot", "create", "reviewer", "--charter", "Reviews Go code.", "--model", "haiku", "--topics", "go"); !strings.Contains(out, "@app/reviewer") {
		t.Fatalf("create: %s", out)
	}
	must(t, proj, "bot", "create", "old", "--global", "--charter", "Retired helper.")
	must(t, proj, "knowledge", "add", "@global/old", "--title", "Style", "use", "gofmt")
	if out := must(t, proj, "bot", "create", "heir", "--charter", "New helper.", "--inherit", "@global/old"); !strings.Contains(out, "inherited 1 knowledge entries from @global/old") {
		t.Fatalf("create --inherit: %s", out)
	}
	if out := must(t, proj, "bot", "inherit", "reviewer", "--from", "@global/old"); !strings.Contains(out, "inherited 1") {
		t.Fatalf("inherit: %s", out)
	}
	list := must(t, proj, "bot", "list")
	for _, want := range []string{"@app/reviewer", "@app/heir", "@global/old", "haiku"} {
		if !strings.Contains(list, want) {
			t.Errorf("list missing %q:\n%s", want, list)
		}
	}
	if out := must(t, proj, "bot", "set", "reviewer", "--model", "opus", "--effort", "high"); !strings.Contains(out, "model opus, effort high") {
		t.Fatalf("set: %s", out)
	}
	if _, err := run(t, proj, "bot", "set", "reviewer", "--effort", "ludicrous"); err == nil {
		t.Fatal("invalid effort accepted")
	}
	show := must(t, proj, "bot", "show", "reviewer")
	if !strings.Contains(show, "model: opus") || !strings.Contains(show, "inherited_from: '@global/old'") || !strings.Contains(show, "knowledge:") {
		t.Fatalf("show:\n%s", show)
	}
	if out := must(t, proj, "bot", "off", "reviewer"); !strings.Contains(out, "is off") {
		t.Fatalf("off: %s", out)
	}
	must(t, proj, "bot", "archive", "reviewer")
	if _, err := run(t, proj, "bot", "on", "reviewer"); err == nil {
		t.Fatal("on after archive should fail")
	}
	if out := must(t, proj, "bot", "restore", "reviewer"); !strings.Contains(out, "is active") {
		t.Fatalf("restore: %s", out)
	}

	kl := must(t, proj, "knowledge", "list", "reviewer")
	id := strings.Fields(kl)[0]
	if !strings.Contains(kl, "(from @global/old)") {
		t.Fatalf("knowledge list: %s", kl)
	}
	if out := must(t, proj, "knowledge", "show", "reviewer", id); !strings.Contains(out, "use gofmt") {
		t.Fatalf("knowledge show: %s", out)
	}
	must(t, proj, "knowledge", "forget", "reviewer", id)
	if out := must(t, proj, "knowledge", "list", "reviewer"); !strings.Contains(out, "no knowledge yet") {
		t.Fatalf("after forget: %s", out)
	}
	if out := must(t, proj, "status"); !strings.Contains(out, "scope:  app") || !strings.Contains(out, "daemon: stopped") {
		t.Fatalf("status: %s", out)
	}
	if out := must(t, home, "status"); !strings.Contains(out, "scope:  global") {
		t.Fatalf("status outside project: %s", out)
	}
	if out := must(t, home, "daemon", "status"); !strings.Contains(out, "stopped") {
		t.Fatalf("daemon status: %s", out)
	}
	if _, err := run(t, proj, "bot", "create", "x"); err == nil {
		t.Fatal("create without charter should fail")
	}
	if _, err := run(t, proj, "mcp"); err == nil {
		t.Fatal("mcp without env should fail")
	}
}

func TestSocketPathFallsBackWhenTooLong(t *testing.T) {
	a := &app{global: config.Scope{Root: "/h/.realm"}}
	if a.socket() != "/h/.realm/realm.sock" {
		t.Fatalf("short socket: %s", a.socket())
	}
	a.global.Root = "/" + strings.Repeat("x", 120)
	if s := a.socket(); len(s) >= maxSocketPath+20 || !strings.HasPrefix(filepath.Base(s), "realm-") {
		t.Fatalf("long socket: %s", s)
	}
}
