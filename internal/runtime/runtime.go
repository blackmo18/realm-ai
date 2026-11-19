// Package runtime defines how one bot turn is executed by a model backend.
package runtime

import (
	"context"
	"encoding/json"
)

// ToolDef describes a tool offered to API-key runtimes.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"input_schema"`
}

// ToolExec runs a tool call and returns its text result.
type ToolExec func(ctx context.Context, name string, args json.RawMessage) (string, error)

// Turn is everything a runner needs for one bot turn.
type Turn struct {
	Model  string
	Effort string

	// SystemPrompt is appended to the CLI default prompt, or used as the
	// system prompt for API runtimes. With ReplaceSystemPrompt the CLI
	// default is replaced (used by the moderator to keep calls small).
	SystemPrompt        string
	ReplaceSystemPrompt bool
	Prompt              string

	// WorkDir is the process working directory (claude-cli) and the root
	// that file tools are confined to (API runtimes).
	WorkDir        string
	AddDirs        []string
	SettingSources string

	// Claude CLI specific.
	BuiltinTools []string // allowed built-in tools; empty = none
	NoTools      bool
	Disallowed   []string // deny rules, e.g. Read(//home/me/.realm/**)
	Skills       []string // allowed skills; "*" = all
	MCPConfig    string   // path to an MCP config exposing realm tools
	SessionID    string
	Resume       bool

	// API runtime specific.
	APITools []ToolDef
	Exec     ToolExec
}

// Result is the outcome of a turn.
type Result struct {
	Text      string
	SessionID string
	CostUSD   float64
}

// Runner executes a turn.
type Runner interface {
	Run(ctx context.Context, t Turn) (Result, error)
}
