// Package bot defines bots, their lifecycle and their on-disk repository.
package bot

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Status is a bot's lifecycle state.
type Status string

// Lifecycle states.
const (
	StatusActive   Status = "active"
	StatusOff      Status = "off"
	StatusArchived Status = "archived"
)

// Role separates normal bots from the chat moderator.
type Role string

// Roles.
const (
	RoleBot       Role = "bot"
	RoleModerator Role = "moderator"
)

// ProviderClaudeCLI runs the bot through the local `claude` CLI and the
// user's Claude subscription.
const ProviderClaudeCLI = "claude-cli"

// ModeratorName is the reserved name of each scope's moderator bot.
const ModeratorName = "moderator"

// Lifecycle actions.
const (
	ActionOn      = "on"
	ActionOff     = "off"
	ActionArchive = "archive"
	ActionRestore = "restore"
)

var (
	nameRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	scopeRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	efforts = []string{"", "low", "medium", "high", "xhigh", "max"}
)

// Lineage records where inherited knowledge came from.
type Lineage struct {
	InheritedFrom string `yaml:"inherited_from"`
	At            string `yaml:"at"`
}

// Bot is the contents of bot.yaml plus where it lives.
type Bot struct {
	Name     string    `yaml:"name"`
	Role     Role      `yaml:"role"`
	Status   Status    `yaml:"status"`
	Provider string    `yaml:"provider"`
	Model    string    `yaml:"model,omitempty"`
	Effort   string    `yaml:"effort,omitempty"`
	Charter  string    `yaml:"charter"`
	Topics   []string  `yaml:"topics,omitempty"`
	Skills   []string  `yaml:"skills,omitempty"`
	Tools    []string  `yaml:"tools,omitempty"`
	Lineage  []Lineage `yaml:"lineage,omitempty"`

	// Scope is the scope name ("global" or a project slug); not stored.
	Scope string `yaml:"-"`
	// Dir is the bot folder; not stored.
	Dir string `yaml:"-"`
}

// Address returns the bot's fully qualified address.
func (b Bot) Address() Address { return Address{Scope: b.Scope, Name: b.Name} }

// Handle returns the bot's fully qualified handle, e.g. "@global/helper".
func (b Bot) Handle() string { return b.Address().String() }

// IsModerator reports whether the bot routes chat messages.
func (b Bot) IsModerator() bool { return b.Role == RoleModerator }

// WithDefaults fills unset fields.
func (b Bot) WithDefaults() Bot {
	out := b
	if out.Role == "" {
		out.Role = RoleBot
	}
	if out.Status == "" {
		out.Status = StatusActive
	}
	if out.Provider == "" {
		out.Provider = ProviderClaudeCLI
	}
	return out
}

// Validate checks bot fields.
func (b Bot) Validate() error {
	if !nameRe.MatchString(b.Name) {
		return fmt.Errorf("invalid bot name %q: use lowercase letters, digits and dashes, starting with a letter (max 32)", b.Name)
	}
	if b.Name == ModeratorName && b.Role != RoleModerator {
		return fmt.Errorf("bot name %q is reserved for the chat moderator", ModeratorName)
	}
	if b.Role != RoleBot && b.Role != RoleModerator {
		return fmt.Errorf("invalid role %q", b.Role)
	}
	switch b.Status {
	case StatusActive, StatusOff, StatusArchived:
	default:
		return fmt.Errorf("invalid status %q", b.Status)
	}
	if b.Provider == "" {
		return errors.New("provider is required")
	}
	if !slices.Contains(efforts, b.Effort) {
		return fmt.Errorf("invalid effort %q (want one of low, medium, high, xhigh, max)", b.Effort)
	}
	if strings.TrimSpace(b.Charter) == "" {
		return errors.New("charter is required: it defines the only work this bot acts on")
	}
	return nil
}

// Transition returns the status after applying a lifecycle action.
func Transition(cur Status, action string) (Status, error) {
	switch action {
	case ActionOn:
		if cur == StatusArchived {
			return cur, errors.New("bot is archived; restore it first")
		}
		return StatusActive, nil
	case ActionOff:
		if cur == StatusArchived {
			return cur, errors.New("bot is archived; restore it first")
		}
		return StatusOff, nil
	case ActionArchive:
		return StatusArchived, nil
	case ActionRestore:
		if cur != StatusArchived {
			return cur, errors.New("bot is not archived")
		}
		return StatusActive, nil
	default:
		return cur, fmt.Errorf("unknown action %q", action)
	}
}

// FirstLine returns the first non-empty line of the charter, for listings.
func (b Bot) FirstLine() string {
	for line := range strings.SplitSeq(b.Charter, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
