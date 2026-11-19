// Package claudecli runs bot turns through the local `claude` CLI in
// headless mode, using the user's Claude subscription.
package claudecli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/blackmo18/realm-ai/internal/runtime"
)

// MCPServerName is the server name realm tools are registered under.
const MCPServerName = "realm"

// Runner executes `claude -p`.
type Runner struct {
	// Bin is the claude executable (default "claude").
	Bin string
	// Env is extra environment for the process (KEY=VALUE).
	Env []string
}

// BuildArgs returns the CLI arguments for a turn. The prompt is sent on stdin.
func BuildArgs(t runtime.Turn) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if t.Model != "" {
		args = append(args, "--model", t.Model)
	}
	if t.Effort != "" {
		args = append(args, "--effort", t.Effort)
	}
	if t.SystemPrompt != "" {
		flag := "--append-system-prompt"
		if t.ReplaceSystemPrompt {
			flag = "--system-prompt"
		}
		args = append(args, flag, t.SystemPrompt)
	}
	switch {
	case t.Resume && t.SessionID != "":
		args = append(args, "--resume", t.SessionID)
	case t.SessionID != "":
		args = append(args, "--session-id", t.SessionID)
	}
	if t.NoTools {
		args = append(args, "--tools", "")
	} else if len(t.BuiltinTools) > 0 {
		args = append(args, "--tools", strings.Join(t.BuiltinTools, ","))
	}
	if allowed := allowedTools(t); len(allowed) > 0 {
		args = append(args, "--allowedTools", strings.Join(allowed, ","))
	}
	args = append(args, "--permission-mode", "dontAsk")
	if t.SettingSources != "" {
		args = append(args, "--setting-sources", t.SettingSources)
	}
	for _, d := range t.AddDirs {
		args = append(args, "--add-dir", d)
	}
	if t.MCPConfig != "" {
		args = append(args, "--mcp-config", t.MCPConfig)
	}
	return append(args, "--strict-mcp-config")
}

func allowedTools(t runtime.Turn) []string {
	if t.NoTools {
		return nil
	}
	allowed := append([]string{}, t.BuiltinTools...)
	for _, s := range t.Skills {
		if s == "*" {
			allowed = append(allowed, "Skill")
			continue
		}
		allowed = append(allowed, "Skill("+s+")")
	}
	if t.MCPConfig != "" {
		allowed = append(allowed, "mcp__"+MCPServerName)
	}
	return allowed
}

// Run executes the turn and parses the stream-json output.
func (r Runner) Run(ctx context.Context, t runtime.Turn) (runtime.Result, error) {
	bin := r.Bin
	if bin == "" {
		bin = "claude"
	}
	cmd := exec.CommandContext(ctx, bin, BuildArgs(t)...)
	cmd.Dir = t.WorkDir
	// Wait for MCP servers before the first model call so realm tools are
	// available on the bot's first turn.
	cmd.Env = append(append(cmd.Environ(), "MCP_CONNECTION_NONBLOCKING=false"), r.Env...)
	cmd.Stdin = strings.NewReader(t.Prompt)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return runtime.Result{}, fmt.Errorf("claude stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return runtime.Result{}, fmt.Errorf("start %s: %w", bin, err)
	}
	res, parseErr := ParseStream(stdout)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return runtime.Result{}, withStderr(parseErr, stderr.String())
	}
	if waitErr != nil && res.Text == "" {
		return runtime.Result{}, withStderr(fmt.Errorf("claude exited: %w", waitErr), stderr.String())
	}
	return res, nil
}

func withStderr(err error, stderr string) error {
	s := strings.TrimSpace(stderr)
	if s == "" {
		return err
	}
	if len(s) > 500 {
		s = s[len(s)-500:]
	}
	return fmt.Errorf("%w: %s", err, s)
}

type streamEvent struct {
	Type      string  `json:"type"`
	Subtype   string  `json:"subtype"`
	SessionID string  `json:"session_id"`
	Result    string  `json:"result"`
	IsError   bool    `json:"is_error"`
	CostUSD   float64 `json:"total_cost_usd"`
	Message   struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// ParseStream reads claude stream-json lines and returns the final result.
func ParseStream(r io.Reader) (runtime.Result, error) {
	var res runtime.Result
	var assistant []string
	gotResult := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.SessionID != "" {
			res.SessionID = ev.SessionID
		}
		switch ev.Type {
		case "assistant":
			for _, c := range ev.Message.Content {
				if c.Type == "text" && c.Text != "" {
					assistant = append(assistant, c.Text)
				}
			}
		case "result":
			gotResult = true
			res.CostUSD = ev.CostUSD
			if ev.IsError {
				msg := ev.Result
				if msg == "" {
					msg = ev.Subtype
				}
				return res, fmt.Errorf("claude turn failed: %s", msg)
			}
			res.Text = ev.Result
		}
	}
	if err := sc.Err(); err != nil {
		return res, fmt.Errorf("read claude output: %w", err)
	}
	if res.Text == "" {
		res.Text = strings.Join(assistant, "\n")
	}
	if !gotResult && res.Text == "" {
		return res, errors.New("claude produced no result")
	}
	return res, nil
}
