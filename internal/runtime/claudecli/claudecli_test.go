package claudecli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/blackmo18/realm-ai/internal/runtime"
)

// TestMain lets the test binary act as a fake `claude` executable when
// FAKE_CLAUDE=1, so runner tests need no real CLI.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_CLAUDE") == "1" {
		fakeClaude()
		return
	}
	os.Exit(m.Run())
}

func fakeClaude() {
	prompt, _ := io.ReadAll(os.Stdin)
	if path := os.Getenv("FAKE_CLAUDE_RECORD"); path != "" {
		wd, _ := os.Getwd()
		_ = os.WriteFile(path, []byte(strings.Join(os.Args[1:], "\x00")+"\n--cwd="+wd+"\n--prompt="+string(prompt)), 0o644)
	}
	switch os.Getenv("FAKE_CLAUDE_MODE") {
	case "error":
		fmt.Println(`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"","session_id":"s-err"}`)
	case "crash":
		fmt.Fprintln(os.Stderr, "boom: not logged in")
		os.Exit(2)
	default:
		fmt.Println(`{"type":"system","subtype":"init","session_id":"s-123"}`)
		fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"thinking"}]}}`)
		fmt.Printf("{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":%q,\"session_id\":\"s-123\",\"total_cost_usd\":0.01}\n", "echo: "+string(prompt))
	}
}

func fakeRunner(t *testing.T, mode string) (Runner, string) {
	t.Helper()
	record := filepath.Join(t.TempDir(), "args")
	return Runner{Bin: os.Args[0], Env: []string{"FAKE_CLAUDE=1", "FAKE_CLAUDE_MODE=" + mode, "FAKE_CLAUDE_RECORD=" + record}}, record
}

func TestBuildArgsFullTurn(t *testing.T) {
	args := BuildArgs(runtime.Turn{
		Model: "sonnet", Effort: "high", SystemPrompt: "charter", SessionID: "abc", Resume: true,
		BuiltinTools: []string{"Read", "Grep"}, Skills: []string{"tdd-guide"}, MCPConfig: "/tmp/mcp.json",
		SettingSources: "user,project", AddDirs: []string{"/code/app"},
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-p --output-format stream-json --verbose", "--model sonnet", "--effort high",
		"--append-system-prompt charter", "--resume abc", "--tools Read,Grep",
		"--allowedTools Read,Grep,Skill(tdd-guide),mcp__realm", "--permission-mode dontAsk",
		"--setting-sources user,project", "--add-dir /code/app", "--mcp-config /tmp/mcp.json", "--strict-mcp-config",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
	if slices.Contains(args, "--dangerously-skip-permissions") {
		t.Fatal("must never skip permissions")
	}
}

func TestBuildArgsModeratorAndNewSession(t *testing.T) {
	args := BuildArgs(runtime.Turn{SystemPrompt: "route", ReplaceSystemPrompt: true, NoTools: true, SessionID: "new-id", Skills: []string{"*"}})
	joined := strings.Join(args, "|")
	if !strings.Contains(joined, "--system-prompt|route") || !strings.Contains(joined, "--session-id|new-id") {
		t.Fatalf("unexpected args: %s", joined)
	}
	if !strings.Contains(joined, "--tools||") {
		t.Fatalf("NoTools should pass an empty --tools: %s", joined)
	}
	if strings.Contains(joined, "--allowedTools") {
		t.Fatalf("NoTools must not allow tools: %s", joined)
	}
	all := strings.Join(BuildArgs(runtime.Turn{Skills: []string{"*"}}), " ")
	if !strings.Contains(all, "--allowedTools Skill") {
		t.Fatalf("wildcard skill: %s", all)
	}
}

func TestRunParsesResultAndSendsPromptOnStdin(t *testing.T) {
	r, record := fakeRunner(t, "ok")
	dir := t.TempDir()
	res, err := r.Run(context.Background(), runtime.Turn{Prompt: "hello", WorkDir: dir, Model: "haiku"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "echo: hello" || res.SessionID != "s-123" || res.CostUSD != 0.01 {
		t.Fatalf("result: %+v", res)
	}
	data, _ := os.ReadFile(record)
	got := string(data)
	resolved, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(got, "--prompt=hello") || !strings.Contains(got, "--model\x00haiku") {
		t.Fatalf("recorded call: %q", got)
	}
	if !strings.Contains(got, "--cwd="+dir) && !strings.Contains(got, "--cwd="+resolved) {
		t.Fatalf("cwd not set: %q", got)
	}
}

func TestRunReportsErrors(t *testing.T) {
	r, _ := fakeRunner(t, "error")
	if _, err := r.Run(context.Background(), runtime.Turn{WorkDir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "error_max_turns") {
		t.Fatalf("want turn error, got %v", err)
	}
	r, _ = fakeRunner(t, "crash")
	if _, err := r.Run(context.Background(), runtime.Turn{WorkDir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("want stderr in error, got %v", err)
	}
	missing := Runner{Bin: filepath.Join(t.TempDir(), "no-such-claude")}
	if _, err := missing.Run(context.Background(), runtime.Turn{}); err == nil {
		t.Fatal("missing binary should fail")
	}
}

func TestParseStreamFallsBackToAssistantText(t *testing.T) {
	in := "noise\n{bad json\n" + `{"type":"assistant","message":{"content":[{"type":"text","text":"part one"},{"type":"tool_use"}]}}` + "\n"
	res, err := ParseStream(strings.NewReader(in))
	if err != nil || res.Text != "part one" {
		t.Fatalf("fallback: %+v %v", res, err)
	}
	if _, err := ParseStream(strings.NewReader("")); err == nil {
		t.Fatal("empty stream should fail")
	}
}
