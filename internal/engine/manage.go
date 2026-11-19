package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/knowledge"
)

// DefaultTools are the read-only built-in tools new bots get.
var DefaultTools = []string{"Read", "Grep", "Glob"}

// BotView is a bot as frontends see it.
type BotView struct {
	Handle         string        `json:"handle"`
	Name           string        `json:"name"`
	Scope          string        `json:"scope"`
	Role           string        `json:"role"`
	Status         string        `json:"status"`
	Provider       string        `json:"provider"`
	Model          string        `json:"model"`
	Effort         string        `json:"effort"`
	Charter        string        `json:"charter"`
	Topics         []string      `json:"topics"`
	Skills         []string      `json:"skills"`
	Tools          []string      `json:"tools"`
	Lineage        []bot.Lineage `json:"lineage"`
	KnowledgeCount int           `json:"knowledge_count"`
	Dir            string        `json:"dir"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// View builds the frontend view of a bot.
func View(b bot.Bot) BotView {
	entries, _ := knowledge.Open(bot.KnowledgeDir(b)).List()
	lineage := b.Lineage
	if lineage == nil {
		lineage = []bot.Lineage{}
	}
	return BotView{
		Handle: b.Handle(), Name: b.Name, Scope: b.Scope, Role: string(b.Role), Status: string(b.Status),
		Provider: b.Provider, Model: b.Model, Effort: b.Effort, Charter: b.Charter,
		Topics: nonNil(b.Topics), Skills: nonNil(b.Skills), Tools: nonNil(b.Tools), Lineage: lineage,
		KnowledgeCount: len(entries), Dir: b.Dir,
	}
}

// BotInput creates a bot.
type BotInput struct {
	Scope    string   `json:"scope"`
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Effort   string   `json:"effort"`
	Charter  string   `json:"charter"`
	Topics   []string `json:"topics"`
	Skills   []string `json:"skills"`
	Tools    []string `json:"tools"` // nil = DefaultTools; empty = none
	Inherit  string   `json:"inherit"`
}

// BotPatch changes a bot; nil fields stay as they are.
type BotPatch struct {
	Provider *string   `json:"provider,omitempty"`
	Model    *string   `json:"model,omitempty"`
	Effort   *string   `json:"effort,omitempty"`
	Charter  *string   `json:"charter,omitempty"`
	Topics   *[]string `json:"topics,omitempty"`
	Skills   *[]string `json:"skills,omitempty"`
	Tools    *[]string `json:"tools,omitempty"`
}

// Apply returns a copy of b with the patch applied.
func (p BotPatch) Apply(b bot.Bot) bot.Bot {
	out := b
	if p.Provider != nil {
		out.Provider = *p.Provider
	}
	if p.Model != nil {
		out.Model = *p.Model
	}
	if p.Effort != nil {
		out.Effort = *p.Effort
	}
	if p.Charter != nil {
		out.Charter = *p.Charter
	}
	if p.Topics != nil {
		out.Topics = *p.Topics
	}
	if p.Skills != nil {
		out.Skills = *p.Skills
	}
	if p.Tools != nil {
		out.Tools = *p.Tools
	}
	return out
}

func (e *Engine) checkProvider(name string) error {
	if name == "" || name == bot.ProviderClaudeCLI {
		return nil
	}
	if _, ok := e.cfg.Providers[name]; !ok {
		return fmt.Errorf("unknown provider %q (configure it in %s)", name, config.Path(e.global.Root))
	}
	return nil
}

// CreateBot creates a bot and optionally copies another bot's knowledge.
func (e *Engine) CreateBot(in BotInput) (BotView, error) {
	if err := e.checkProvider(in.Provider); err != nil {
		return BotView{}, err
	}
	repo, err := e.Repo(in.Scope)
	if err != nil {
		return BotView{}, err
	}
	tools := in.Tools
	if tools == nil {
		tools = DefaultTools
	}
	b, err := repo.Create(bot.Bot{Name: in.Name, Provider: in.Provider, Model: in.Model, Effort: in.Effort,
		Charter: in.Charter, Topics: in.Topics, Skills: in.Skills, Tools: tools})
	if err != nil {
		return BotView{}, err
	}
	if in.Inherit != "" {
		if _, err := e.InheritKnowledge(b.Handle(), in.Inherit); err != nil {
			return BotView{}, fmt.Errorf("created %s but inherit failed: %w", b.Handle(), err)
		}
		if b, err = repo.Get(b.Name); err != nil {
			return BotView{}, err
		}
	}
	return View(b), nil
}

// GetBot returns one bot.
func (e *Engine) GetBot(handle, ctxScope string) (BotView, error) {
	b, err := e.ResolveBot(handle, ctxScope)
	if err != nil {
		return BotView{}, err
	}
	return View(b), nil
}

// ListBots lists bots visible from a scope.
func (e *Engine) ListBots(scope string) ([]BotView, error) {
	bots, err := e.VisibleBots(scope)
	if err != nil {
		return nil, err
	}
	out := make([]BotView, 0, len(bots))
	for _, b := range bots {
		out = append(out, View(b))
	}
	return out, nil
}

// UpdateBot applies a patch to a bot.
func (e *Engine) UpdateBot(handle string, p BotPatch) (BotView, error) {
	if p.Provider != nil {
		if err := e.checkProvider(*p.Provider); err != nil {
			return BotView{}, err
		}
	}
	b, err := e.ResolveBot(handle, "")
	if err != nil {
		return BotView{}, err
	}
	repo, err := e.Repo(b.Scope)
	if err != nil {
		return BotView{}, err
	}
	updated, err := repo.Update(b.Name, p.Apply)
	if err != nil {
		return BotView{}, err
	}
	return View(updated), nil
}

// SetBotStatus runs a lifecycle action. Turning a bot on dispatches its
// queued messages; the count is returned.
func (e *Engine) SetBotStatus(handle, action string) (BotView, int, error) {
	b, err := e.ResolveBot(handle, "")
	if err != nil {
		return BotView{}, 0, err
	}
	repo, err := e.Repo(b.Scope)
	if err != nil {
		return BotView{}, 0, err
	}
	updated, err := repo.Apply(b.Name, action)
	if err != nil {
		return BotView{}, 0, err
	}
	n, err := e.BotChanged(updated.Handle())
	return View(updated), n, err
}

// InheritKnowledge copies src's knowledge into dst and records lineage.
func (e *Engine) InheritKnowledge(dstHandle, srcHandle string) (int, error) {
	dst, err := e.ResolveBot(dstHandle, "")
	if err != nil {
		return 0, err
	}
	src, err := e.ResolveBot(srcHandle, dst.Scope)
	if err != nil {
		return 0, err
	}
	if src.Handle() == dst.Handle() {
		return 0, errors.New("a bot cannot inherit from itself")
	}
	n, err := knowledge.Open(bot.KnowledgeDir(dst)).CopyFrom(knowledge.Open(bot.KnowledgeDir(src)), src.Handle())
	if err != nil {
		return 0, err
	}
	repo, err := e.Repo(dst.Scope)
	if err != nil {
		return 0, err
	}
	_, err = repo.Update(dst.Name, func(b bot.Bot) bot.Bot {
		out := b
		out.Lineage = append(append([]bot.Lineage{}, b.Lineage...), bot.Lineage{InheritedFrom: src.Handle(), At: time.Now().UTC().Format("2006-01-02")})
		return out
	})
	return n, err
}

func (e *Engine) knowledgeOf(handle string) (*knowledge.Store, error) {
	b, err := e.ResolveBot(handle, "")
	if err != nil {
		return nil, err
	}
	return knowledge.Open(bot.KnowledgeDir(b)), nil
}

// Knowledge lists a bot's knowledge entries.
func (e *Engine) Knowledge(handle string) ([]knowledge.Entry, error) {
	ks, err := e.knowledgeOf(handle)
	if err != nil {
		return nil, err
	}
	entries, err := ks.List()
	if entries == nil {
		entries = []knowledge.Entry{}
	}
	return entries, err
}

// AddKnowledge adds an entry to a bot's knowledge.
func (e *Engine) AddKnowledge(handle, title, body string) (knowledge.Entry, error) {
	ks, err := e.knowledgeOf(handle)
	if err != nil {
		return knowledge.Entry{}, err
	}
	return ks.Remember(title, body)
}

// UpdateKnowledge rewrites one entry.
func (e *Engine) UpdateKnowledge(handle, id, title, body string) (knowledge.Entry, error) {
	ks, err := e.knowledgeOf(handle)
	if err != nil {
		return knowledge.Entry{}, err
	}
	return ks.Update(id, title, body)
}

// ForgetKnowledge deletes one entry.
func (e *Engine) ForgetKnowledge(handle, id string) error {
	ks, err := e.knowledgeOf(handle)
	if err != nil {
		return err
	}
	return ks.Forget(id)
}

// ChatMembers returns a chat's routable bots.
func (e *Engine) ChatMembers(chatID string) ([]BotView, error) {
	c, err := e.GetChat(chatID)
	if err != nil {
		return nil, err
	}
	bots, err := e.Members(c)
	if err != nil {
		return nil, err
	}
	out := make([]BotView, 0, len(bots))
	for _, b := range bots {
		out = append(out, View(b))
	}
	return out, nil
}

// Skill is a skill a bot may be allowed to use.
type Skill struct {
	Name        string `json:"name"`
	Source      string `json:"source"` // "global" or "project"
	Description string `json:"description"`
}

// ListSkills lists skills in ~/.claude/skills and, for a project scope,
// <project>/.claude/skills.
func (e *Engine) ListSkills(scope string) ([]Skill, error) {
	var out []Skill
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, readSkills(filepath.Join(home, ".claude", "skills"), "global")...)
	}
	if dir := e.projectDir(scope); dir != "" {
		out = append(out, readSkills(filepath.Join(dir, ".claude", "skills"), "project")...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if out == nil {
		out = []Skill{}
	}
	return out, nil
}

func readSkills(dir, source string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, en := range entries {
		data, err := os.ReadFile(filepath.Join(dir, en.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		out = append(out, Skill{Name: en.Name(), Source: source, Description: skillDescription(string(data))})
	}
	return out
}

func skillDescription(md string) string {
	for line := range strings.SplitSeq(md, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "description:"); ok {
			d := strings.Trim(strings.TrimSpace(v), `"'`)
			if len(d) > 160 {
				d = d[:160] + "…"
			}
			return d
		}
	}
	return ""
}

// ProviderInfo describes a runtime without exposing keys.
type ProviderInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	KeyEnv string `json:"key_env"`
	Ready  bool   `json:"ready"`
	Note   string `json:"note"`
}

// Providers lists runtimes and whether each is usable.
func (e *Engine) Providers() []ProviderInfo {
	cli := ProviderInfo{Name: bot.ProviderClaudeCLI, Type: "subscription"}
	if path, err := exec.LookPath(e.cfg.ClaudeBin); err == nil {
		cli.Ready, cli.Note = true, path
	} else {
		cli.Note = e.cfg.ClaudeBin + " not found on PATH"
	}
	out := []ProviderInfo{cli}
	names := make([]string, 0, len(e.cfg.Providers))
	for n := range e.cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := e.cfg.Providers[n]
		info := ProviderInfo{Name: n, Type: p.Type, KeyEnv: p.APIKeyEnv}
		switch {
		case p.APIKeyEnv == "":
			info.Ready, info.Note = true, "no key needed"
		case os.Getenv(p.APIKeyEnv) != "":
			info.Ready, info.Note = true, p.APIKeyEnv+" set"
		default:
			info.Note = p.APIKeyEnv + " not set"
		}
		out = append(out, info)
	}
	return out
}

// Settings is the read-only configuration frontends show.
type Settings struct {
	Home           string        `json:"home"`
	ModeratorModel string        `json:"moderator_model"`
	Limits         config.Limits `json:"limits"`
	MaxFanout      int           `json:"max_fanout_per_turn"`
	MaxTurns       int           `json:"max_turns_per_message"`
}

// Settings returns the effective configuration.
func (e *Engine) Settings() Settings {
	return Settings{Home: e.global.Root, ModeratorModel: e.cfg.ModeratorModel, Limits: e.cfg.Limits,
		MaxFanout: maxFanoutPerTurn, MaxTurns: maxTurnsPerMessage}
}
