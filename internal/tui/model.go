package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blackmo18/realm-ai/internal/store"
)

type itemKind int

const (
	itemScope itemKind = iota
	itemChat
	itemBots
	itemNewChat
	itemAddProject
)

type treeItem struct {
	kind   itemKind
	scope  string
	chatID string
	label  string
}

type view int

const (
	viewHome view = iota
	viewChat
	viewBots
	viewForm
)

const treeWidth = 30

// Messages.
type (
	treeLoaded struct {
		items []treeItem
		err   error
	}
	resultMsg struct {
		info     string
		err      error
		openChat string
		openBots string
		reload   bool
	}
)

// Model is the root Bubble Tea model.
type Model struct {
	api       API
	ctx       context.Context
	scope     string // scope of the folder realm tui was started in
	width     int
	height    int
	tree      []treeItem
	cursor    int
	focusMain bool
	view      view
	chat      chatState
	bots      botsState
	form      form
	formBack  view
	status    string
	isError   bool
	stop      context.CancelFunc
}

// New creates the TUI model. scope selects the project the TUI opens on.
func New(ctx context.Context, api API, scope string) Model {
	return Model{api: api, ctx: ctx, scope: scope, chat: newChatState()}
}

// Init loads the scope tree.
func (m Model) Init() tea.Cmd { return m.loadTree() }

func (m Model) loadTree() tea.Cmd {
	return func() tea.Msg {
		projects, err := m.api.Projects(m.ctx)
		if err != nil {
			return treeLoaded{err: err}
		}
		scopes := []string{"global"}
		for _, p := range projects {
			scopes = append(scopes, p.Slug)
		}
		var items []treeItem
		for _, s := range scopes {
			items = append(items, treeItem{kind: itemScope, scope: s, label: s})
			chats, err := m.api.ListChats(m.ctx, s)
			if err != nil {
				return treeLoaded{err: err}
			}
			for _, c := range chats {
				if c.Kind == store.KindGroup {
					items = append(items, treeItem{kind: itemChat, scope: s, chatID: c.ID, label: "# " + c.Name})
				}
			}
			items = append(items, treeItem{kind: itemBots, scope: s, label: "bots"})
			for _, c := range chats {
				if c.Kind == store.KindDM {
					name := strings.NewReplacer("@user+", "", "+@user", "").Replace(c.Name)
					items = append(items, treeItem{kind: itemChat, scope: s, chatID: c.ID, label: "dm " + name})
				}
			}
			items = append(items, treeItem{kind: itemNewChat, scope: s, label: "+ new chat"})
		}
		items = append(items, treeItem{kind: itemAddProject, label: "+ add project…"})
		return treeLoaded{items: items}
	}
}

func (m Model) flash(info string, err error) Model {
	out := m
	if err != nil {
		out.status, out.isError = err.Error(), true
	} else {
		out.status, out.isError = info, false
	}
	return out
}

// currentScope is the scope of the tree selection (or the start scope).
func (m Model) currentScope() string {
	if m.cursor < len(m.tree) && m.tree[m.cursor].scope != "" {
		return m.tree[m.cursor].scope
	}
	if m.scope != "" {
		return m.scope
	}
	return "global"
}

// Update routes messages and keys.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		out := m
		out.width, out.height = msg.Width, msg.Height
		out.chat = out.chat.resize(out.mainWidth())
		return out, nil
	case treeLoaded:
		if msg.err != nil {
			return m.flash("", msg.err), nil
		}
		out := m
		out.tree = msg.items
		if out.cursor >= len(out.tree) {
			out.cursor = 0
		}
		return out, nil
	case resultMsg:
		return m.applyResult(msg)
	case chatLoaded, streamMsg, streamClosed:
		return m.updateChatMsg(msg)
	case botsLoaded, knowledgeLoaded:
		return m.updateBotsMsg(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) applyResult(r resultMsg) (tea.Model, tea.Cmd) {
	out := m.flash(r.info, r.err)
	if r.err != nil {
		if out.view == viewForm {
			out.form.err = r.err.Error()
		}
		return out, nil
	}
	var cmds []tea.Cmd
	if r.reload {
		cmds = append(cmds, out.loadTree())
	}
	switch {
	case r.openChat != "":
		next, cmd := out.openChat(r.openChat)
		return next, tea.Batch(append(cmds, cmd)...)
	case r.openBots != "":
		next, cmd := out.openBots(r.openBots)
		return next, tea.Batch(append(cmds, cmd)...)
	case out.view == viewForm:
		out.view = out.formBack
	}
	if out.view == viewBots {
		cmds = append(cmds, out.loadBots(out.bots.scope))
	}
	return out, tea.Batch(cmds...)
}

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m.quit()
	}
	if m.view == viewForm {
		f, cmd, cancelled := m.form.update(k)
		out := m
		out.form = f
		if cancelled {
			out.view = out.formBack
		}
		return out, cmd
	}
	if m.focusMain && m.view == viewChat {
		return m.chatKey(k)
	}
	if m.focusMain && m.view == viewBots {
		return m.botsKey(k)
	}
	return m.treeKey(k)
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.stop != nil {
		m.stop()
	}
	return m, tea.Quit
}

func (m Model) treeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	out := m
	switch k.String() {
	case "q":
		return m.quit()
	case "up", "k":
		if out.cursor > 0 {
			out.cursor--
		}
	case "down", "j":
		if out.cursor < len(out.tree)-1 {
			out.cursor++
		}
	case "tab", "right":
		if out.view == viewChat || out.view == viewBots {
			out.focusMain = true
		}
	case "n":
		return out.newChatForm(out.currentScope())
	case "p":
		return out.projectForm()
	case "b":
		return out.openBots(out.currentScope())
	case "enter":
		if out.cursor >= len(out.tree) {
			return out, nil
		}
		it := out.tree[out.cursor]
		switch it.kind {
		case itemChat:
			return out.openChat(it.chatID)
		case itemBots:
			return out.openBots(it.scope)
		case itemNewChat:
			return out.newChatForm(it.scope)
		case itemAddProject:
			return out.projectForm()
		}
	}
	return out, nil
}

func (m Model) mainWidth() int {
	w := m.width - treeWidth - 6
	if w < 30 {
		w = 30
	}
	return w
}

func (m Model) bodyHeight() int {
	h := m.height - 4
	if h < 10 {
		h = 10
	}
	return h
}

// View renders the panes and footer.
func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	tree := m.treeView()
	var main string
	switch m.view {
	case viewChat:
		main = m.chatView()
	case viewBots:
		main = m.botsView()
	case viewForm:
		main = m.form.view(m.mainWidth())
	default:
		main = titleStyle.Render("Realm") + "\n\n" + mutedStyle.Render("Pick a chat or bots on the left.\nn new chat · p add project · b bots · q quit")
	}
	treeStyle, mainStyle := activePane, paneStyle
	if m.focusMain || m.view == viewForm {
		treeStyle, mainStyle = paneStyle, activePane
	}
	h := m.bodyHeight()
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		treeStyle.Width(treeWidth).Height(h).Render(tree),
		mainStyle.Width(m.mainWidth()).Height(h).Render(main))
	status := ""
	if m.status != "" {
		st := mutedStyle
		if m.isError {
			st = badStyle
		}
		status = st.Render(" " + m.status)
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, status, m.footerView())
}

func (m Model) treeView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Scopes") + "\n")
	for i, it := range m.tree {
		line := "  " + it.label
		switch it.kind {
		case itemScope:
			line = mutedStyle.Render(it.label)
		case itemNewChat, itemAddProject:
			line = "  " + titleStyle.Render(it.label)
		}
		if i == m.cursor {
			line = selStyle.Render(fmt.Sprintf("%-*s", treeWidth-2, "▸ "+strings.TrimLeft(it.label, " ")))
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m Model) footerView() string {
	switch {
	case m.view == viewForm:
		return footer(m.width, "tab", "next field", "←→", "choose", "space", "toggle", "ctrl+s", "save", "esc", "cancel")
	case m.focusMain && m.view == viewChat:
		return footer(m.width, "enter", "send", "alt+enter", "new line", "1-9", "pick bot", "r", "reroute", "m", "routing", "esc", "back", "(letters/numbers act when the box is empty)")
	case m.focusMain && m.view == viewBots:
		return footer(m.width, "a", "add bot", "o", "on/off", "x", "archive", "i", "inherit", "k", "knowledge", "d", "DM", "esc", "back")
	default:
		return footer(m.width, "↑↓", "move", "enter", "open", "tab", "pane", "n", "new chat", "p", "add project", "b", "bots", "q", "quit")
	}
}
