package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/store"
)

var (
	builtinTools = []string{"Read", "Grep", "Glob", "WebFetch", "WebSearch", "Edit", "Write", "Bash"}
	riskyTools   = []string{"Edit", "Write", "Bash"}
	efforts      = []string{"default", "low", "medium", "high", "xhigh", "max"}
	providers    = []string{"claude-cli", "anthropic", "openai", "openrouter", "ollama"}
	routings     = []string{store.RoutingModerated, store.RoutingMention}
)

func (m Model) scopes() []string {
	out := []string{"global"}
	for _, it := range m.tree {
		if it.kind == itemScope && it.scope != "global" {
			out = append(out, it.scope)
		}
	}
	return out
}

func (m Model) showForm(f form) (tea.Model, tea.Cmd) {
	out := m
	if out.view != viewForm {
		out.formBack = out.view
	}
	out.form, out.view = f, viewForm
	return out, nil
}

func indexOr(list []string, v string) int {
	if i := slices.Index(list, v); i >= 0 {
		return i
	}
	return 0
}

// globalBotNames lists active global bots for member pickers.
func (m Model) globalBotNames() []string {
	var out []string
	for _, b := range m.chat.bots {
		if b.Scope == "global" && b.Status == "active" && b.Role != "moderator" {
			out = append(out, b.Handle)
		}
	}
	for _, b := range m.bots.list {
		if b.Scope == "global" && b.Status == "active" && !slices.Contains(out, b.Handle) {
			out = append(out, b.Handle)
		}
	}
	return out
}

func (m Model) newChatForm(scope string) (tea.Model, tea.Cmd) {
	scopes := m.scopes()
	fields := []field{
		textField("name", "Name", "", "release"),
		choiceField("scope", "Scope", scopes, indexOr(scopes, scope)),
		choiceField("routing", "Routing", routings, 0),
	}
	fields[2].hint = func(f form) string {
		if f.value("routing") == store.RoutingMention {
			return "only @mentions reach bots — zero model calls otherwise"
		}
		return "messages without @ cost one small moderator call; it routes or asks you"
	}
	if g := m.globalBotNames(); len(g) > 0 {
		fields = append(fields, checksField("members", "Global bots to add", g))
	}
	api, ctx := m.api, m.ctx
	return m.showForm(newForm("New group chat", fields, func(f form) tea.Cmd {
		return func() tea.Msg {
			c, err := api.CreateChat(ctx, daemon.ChatCreate{Scope: f.value("scope"), Name: f.value("name"), Routing: f.value("routing"), Members: f.values("members")})
			return resultMsg{info: "created " + c.ID, err: err, openChat: c.ID, reload: err == nil}
		}
	}))
}

func (m Model) botForm(scope string) (tea.Model, tea.Cmd) {
	scopes := m.scopes()
	fields := []field{
		textField("name", "Name", "", "reviewer"),
		choiceField("scope", "Scope", scopes, indexOr(scopes, scope)),
		textField("charter", "Charter (the only work this bot acts on)", "", "Reviews Go code in this repo. Refuses everything else."),
		choiceField("provider", "Runtime", providers, 0),
		textField("model", "Model", "", "haiku / sonnet / opus or a provider model id"),
		choiceField("effort", "Effort", efforts, 0),
		textField("topics", "Topics (comma separated)", "", "go, code-review"),
		checksField("tools", "Built-in tools", builtinTools, "Read", "Grep", "Glob"),
	}
	fields[0].hint = func(f form) string { return "handle: @" + f.value("scope") + "/" + f.value("name") }
	api, ctx := m.api, m.ctx
	bf := newForm("New bot", fields, func(f form) tea.Cmd {
		return func() tea.Msg {
			effort := f.value("effort")
			if effort == "default" {
				effort = ""
			}
			v, err := api.CreateBot(ctx, engine.BotInput{Scope: f.value("scope"), Name: f.value("name"), Charter: f.value("charter"),
				Provider: f.value("provider"), Model: f.value("model"), Effort: effort, Topics: splitList(f.value("topics")), Tools: f.values("tools")})
			return resultMsg{info: "created " + v.Handle, err: err, openBots: f.value("scope")}
		}
	})
	bf.note = func(f form) string {
		var risky []string
		for _, t := range f.values("tools") {
			if slices.Contains(riskyTools, t) {
				risky = append(risky, t)
			}
		}
		if len(risky) == 0 {
			return ""
		}
		return "! " + strings.Join(risky, ", ") + " runs as your user: it can change files and reach other bots' data. Grant only if trusted."
	}
	return m.showForm(bf)
}

func (m Model) projectForm() (tea.Model, tea.Cmd) {
	api, ctx := m.api, m.ctx
	templates, err := api.Templates(ctx)
	if err != nil {
		return m.flash("", err), nil
	}
	names := make([]string, len(templates))
	for i, t := range templates {
		names[i] = t.Name
	}
	fields := []field{
		textField("dir", "Project folder", "", "~/code/my-app"),
		textField("slug", "Project name (blank = folder name)", "", ""),
		checksField("ignore", "Git", []string{"keep chat history out of git"}, "keep chat history out of git"),
		checksField("starters", "Starter bots", names, "reviewer"),
	}
	if g := m.globalBotNames(); len(g) > 0 {
		fields = append(fields, checksField("globals", "Global bots for the first chat", g))
	}
	fields = append(fields, textField("chat", "First chat", "general", ""), choiceField("routing", "Routing", routings, 0))
	inspected := map[string]string{} // View calls hints often; ask the daemon once per path
	fields[0].hint = func(f form) string {
		dir := f.value("dir")
		if dir == "" {
			return ""
		}
		if h, ok := inspected[dir]; ok {
			return h
		}
		h := describeFolder(api.InspectFolder(ctx, dir))
		inspected[dir] = h
		return h
	}
	return m.showForm(newForm("Add a project", fields, func(f form) tea.Cmd {
		return func() tea.Msg {
			if f.value("dir") == "" {
				return resultMsg{err: errors.New("project folder is required")}
			}
			r, err := api.Onboard(ctx, engine.OnboardInput{Dir: f.value("dir"), Slug: f.value("slug"), IgnoreDB: len(f.values("ignore")) > 0,
				Starters: f.values("starters"), GlobalMembers: f.values("globals"), ChatName: f.value("chat"), Routing: f.value("routing")})
			return resultMsg{info: r.Project + " is ready", err: err, openChat: r.Chat.ID, reload: err == nil}
		}
	}))
}

func describeFolder(info engine.FolderInfo, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case !info.Exists:
		return "✕ folder not found"
	case info.Registered:
		return "✕ already registered as " + info.Slug
	}
	return fmt.Sprintf("git: %v · CLAUDE.md: %v · project skills: %d · will be @%s", info.IsGit, info.HasClaudeMD, info.SkillCount, info.Slug)
}

func (m Model) inheritForm(dst engine.BotView) (tea.Model, tea.Cmd) {
	var sources []string
	for _, b := range m.bots.list {
		if b.Handle != dst.Handle {
			sources = append(sources, b.Handle)
		}
	}
	if len(sources) == 0 {
		return m.flash("no other bots to inherit from", nil), nil
	}
	api, ctx := m.api, m.ctx
	return m.showForm(newForm("Inherit knowledge into "+dst.Handle, []field{choiceField("from", "From", sources, 0)}, func(f form) tea.Cmd {
		return func() tea.Msg {
			n, err := api.InheritKnowledge(ctx, dst.Handle, f.value("from"))
			return resultMsg{info: fmt.Sprintf("copied %d notes from %s", n, f.value("from")), err: err, openBots: m.bots.scope}
		}
	}))
}

func (m Model) dmForm(b engine.BotView) (tea.Model, tea.Cmd) {
	api, ctx := m.api, m.ctx
	return m.showForm(newForm("Direct message "+b.Handle, []field{textField("text", "Message", "", "")}, func(f form) tea.Cmd {
		return func() tea.Msg {
			if f.value("text") == "" {
				return resultMsg{err: errors.New("message is empty")}
			}
			msg, err := api.DM(ctx, b.Handle, b.Scope, f.value("text"))
			return resultMsg{err: err, openChat: msg.Chat, reload: err == nil}
		}
	}))
}
