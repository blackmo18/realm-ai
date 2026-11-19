package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealmDefsAreComplete(t *testing.T) {
	want := map[string]bool{SendDM: true, Post: true, ReadChat: true, ListBots: true, Delegate: true, Remember: true, Recall: true, Forget: true}
	for _, d := range Realm() {
		if !want[d.Name] {
			t.Errorf("unexpected tool %q", d.Name)
		}
		if req, isList := d.Schema["required"].([]string); !isList || req == nil {
			t.Errorf("tool %q: required must be a JSON array, got %#v", d.Name, d.Schema["required"])
		}
		if d.Schema["type"] != "object" || d.Description == "" {
			t.Errorf("tool %q has bad schema or description", d.Name)
		}
		delete(want, d.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing tools: %v", want)
	}
	if len(FileDefs()) != 3 || !IsFileTool(Grep) || IsFileTool(Remember) {
		t.Fatal("file tool defs wrong")
	}
}

func project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755))
	must(os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	must(os.WriteFile(filepath.Join(root, "internal", "auth", "jwt.go"), []byte("package auth\n// TokenTTL is 1h\nconst TokenTTL = 3600\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, ".git", "config"), []byte("TokenTTL secret"), 0o644))
	must(os.WriteFile(filepath.Join(filepath.Dir(root), "outside.txt"), []byte("secret"), 0o644))
	return root
}

func run(t *testing.T, f Files, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return f.Exec(name, raw)
}

func TestFilesReadGrepGlob(t *testing.T) {
	f := Files{Root: project(t)}
	out, err := run(t, f, ReadFile, map[string]any{"path": "internal/auth/jwt.go"})
	if err != nil || !strings.Contains(out, "TokenTTL = 3600") {
		t.Fatalf("read: %q %v", out, err)
	}
	out, err = run(t, f, Grep, map[string]any{"pattern": "TokenTTL"})
	if err != nil || !strings.Contains(out, "internal/auth/jwt.go:2:") || strings.Contains(out, ".git") {
		t.Fatalf("grep: %q %v", out, err)
	}
	if out, _ := run(t, f, Grep, map[string]any{"pattern": "nothing-here"}); out != "no matches" {
		t.Fatalf("grep no match: %q", out)
	}
	out, err = run(t, f, Glob, map[string]any{"pattern": "internal/*/*.go"})
	if err != nil || out != "internal/auth/jwt.go" {
		t.Fatalf("glob: %q %v", out, err)
	}
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "id_rsa"), []byte("PRIVATE KEY TokenTTL"), 0o600)
	_ = os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(f.Root, "notes"))
	_ = os.Symlink(outside, filepath.Join(f.Root, "ext"))
	if out, _ := run(t, f, Grep, map[string]any{"pattern": "PRIVATE"}); out != "no matches" {
		t.Fatalf("grep followed a symlink out of the project: %q", out)
	}
	if out, _ := run(t, f, Glob, map[string]any{"pattern": "ext/*"}); out != "no matches" {
		t.Fatalf("glob listed files outside the project: %q", out)
	}
	if out, _ := run(t, f, Glob, map[string]any{"pattern": "*.rs"}); out != "no matches" {
		t.Fatalf("glob no match: %q", out)
	}
}

func TestFilesRejectEscapes(t *testing.T) {
	root := project(t)
	f := Files{Root: root}
	if err := os.Symlink(filepath.Join(filepath.Dir(root), "outside.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	bad := []struct {
		name string
		args map[string]any
	}{
		{ReadFile, map[string]any{"path": "../outside.txt"}},
		{ReadFile, map[string]any{"path": "/etc/passwd"}},
		{ReadFile, map[string]any{"path": "link.txt"}},
		{Grep, map[string]any{"pattern": "x", "path": ".."}},
		{Grep, map[string]any{"pattern": "("}},
		{Glob, map[string]any{"pattern": "../*"}},
		{"rm", map[string]any{}},
	}
	for _, b := range bad {
		if out, err := run(t, f, b.name, b.args); err == nil {
			t.Errorf("%s %v: expected error, got %q", b.name, b.args, out)
		}
	}
	if _, err := (Files{}).Exec(ReadFile, []byte(`{}`)); err == nil {
		t.Fatal("no root should fail")
	}
	if _, err := f.Exec(ReadFile, []byte(`not json`)); err == nil {
		t.Fatal("bad json should fail")
	}
}
