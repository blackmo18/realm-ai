package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/knowledge"
)

type botsState struct {
	scope     string
	list      []engine.BotView
	cursor    int
	knowledge []knowledge.Entry
	showKnow  bool
	confirm   string // handle awaiting archive confirmation
}

type (
	botsLoaded struct {
		scope string
		bots  []engine.BotView
		err   error
	}
	knowledgeLoaded struct {
		entries []knowledge.Entry
		err     error
	}
)

func (m Model) loadBots(scope string) tea.Cmd {
	api, ctx := m.api, m.ctx
	return func() tea.Msg {
		bots, err := api.ListBots(ctx, scope)
		var visible []engine.BotView
		for _, b := range bots {
			if b.Role != "moderator" {
				visible = append(visible, b)
			}
		}
		return botsLoaded{scope: scope, bots: visible, err: err}
	}
}

func (m Model) openBots(scope string) (tea.Model, tea.Cmd) {
	out := m
	out.view, out.focusMain = viewBots, true
	out.bots = botsState{scope: scope}
	return out, out.loadBots(scope)
}

func (m Model) updateBotsMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	out := m
	switch msg := msg.(type) {
	case botsLoaded:
		if msg.err != nil {
			return out.flash("", msg.err), nil
		}
		if msg.scope == out.bots.scope {
			out.bots.list = msg.bots
			if out.bots.cursor >= len(msg.bots) {
				out.bots.cursor = 0
			}
		}
	case knowledgeLoaded:
		if msg.err != nil {
			return out.flash("", msg.err), nil
		}
		out.bots.knowledge, out.bots.showKnow = msg.entries, true
	}
	return out, nil
}

func (m Model) selectedBot() (engine.BotView, bool) {
	if m.bots.cursor < len(m.bots.list) {
		return m.bots.list[m.bots.cursor], true
	}
	return engine.BotView{}, false
}

func (m Model) botsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	out := m
	b, has := out.selectedBot()
	if out.bots.confirm != "" {
		handle := out.bots.confirm
		out.bots.confirm = ""
		if k.String() == "y" {
			return out, out.setStatus(handle, "archive")
		}
		return out.flash("archive cancelled", nil), nil
	}
	switch k.String() {
	case "esc":
		out.focusMain = false
	case "up", "k":
		if out.bots.cursor > 0 {
			out.bots.cursor--
			out.bots.showKnow = false
		}
	case "down", "j":
		if out.bots.cursor < len(out.bots.list)-1 {
			out.bots.cursor++
			out.bots.showKnow = false
		}
	case "a":
		return out.botForm(out.bots.scope)
	case "o":
		if has {
			action := map[string]string{"active": "off", "off": "on", "archived": "restore"}[b.Status]
			return out, out.setStatus(b.Handle, action)
		}
	case "x":
		if has && b.Status != "archived" {
			out.bots.confirm = b.Handle
			return out.flash(fmt.Sprintf("Archive %s? It will reject messages; knowledge is kept. y to confirm", b.Handle), nil), nil
		}
	case "i":
		if has {
			return out.inheritForm(b)
		}
	case "d":
		if has {
			return out.dmForm(b)
		}
	case "enter", "K":
		if has {
			api, ctx := out.api, out.ctx
			return out, func() tea.Msg {
				e, err := api.Knowledge(ctx, b.Handle)
				return knowledgeLoaded{entries: e, err: err}
			}
		}
	}
	return out, nil
}

func (m Model) setStatus(handle, action string) tea.Cmd {
	api, ctx := m.api, m.ctx
	return func() tea.Msg {
		r, err := api.SetBotStatus(ctx, handle, action)
		info := fmt.Sprintf("%s is %s", handle, r.Bot.Status)
		if r.Dispatched > 0 {
			info += fmt.Sprintf(" · %d queued message(s) delivered", r.Dispatched)
		}
		return resultMsg{info: info, err: err, openBots: m.bots.scope}
	}
}

func (m Model) botsView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Bots · "+m.bots.scope) + "\n")
	b.WriteString(faintStyle.Render(fmt.Sprintf("  %-26s %-9s %s", "HANDLE", "STATUS", "MODEL")) + "\n")
	for i, bot := range m.bots.list {
		model := bot.Model
		if model == "" {
			model = "default"
		}
		if bot.Effort != "" {
			model += "·" + bot.Effort
		}
		line := fmt.Sprintf("%-26s %s %s", bot.Handle, statusStyle(bot.Status).Render(fmt.Sprintf("%-9s", bot.Status)), model)
		if i == m.bots.cursor {
			line = selStyle.Render("▸ ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	if len(m.bots.list) == 0 {
		b.WriteString(mutedStyle.Render("  No bots yet — press a to add one.") + "\n")
	}
	if bot, ok := m.selectedBot(); ok {
		b.WriteString("\n" + mutedStyle.Render(fmt.Sprintf("  %s · %s · tools %s", bot.Handle, bot.Provider, strings.Join(bot.Tools, ","))) + "\n")
		b.WriteString(mutedStyle.Width(m.mainWidth()-6).Render("  "+bot.Charter) + "\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  knowledge: %d notes · topics: %s", bot.KnowledgeCount, strings.Join(bot.Topics, ", "))) + "\n")
		if m.bots.showKnow {
			b.WriteString("\n" + titleStyle.Render("Knowledge") + "\n")
			for _, e := range m.bots.knowledge {
				origin := ""
				if e.Origin != "" {
					origin = faintStyle.Render(" (from " + e.Origin + ")")
				}
				b.WriteString("  " + userStyle.Render(e.Title) + origin + "\n")
				b.WriteString(mutedStyle.Width(m.mainWidth()-8).Render("    "+e.Body) + "\n")
			}
			if len(m.bots.knowledge) == 0 {
				b.WriteString(mutedStyle.Render("  no notes yet") + "\n")
			}
		}
	}
	return b.String()
}
