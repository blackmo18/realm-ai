package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/store"
)

const maxComposerLines = 6

type chatState struct {
	chat       store.Chat
	msgs       []store.Message
	bots       []engine.BotView
	input      textarea.Model
	mention    int
	rerouting  bool
	rerouteIdx int
}

type (
	chatLoaded struct {
		chat store.Chat
		msgs []store.Message
		bots []engine.BotView
		err  error
	}
	streamMsg struct {
		chat string
		msg  store.Message
		ch   <-chan store.Message
	}
	streamClosed struct{ chat string }
)

func newChatState() chatState {
	in := textarea.New()
	in.Placeholder = "Message — @mention a bot, or let the moderator route it"
	in.ShowLineNumbers = false
	in.Prompt = "› "
	in.CharLimit = 8000
	in.SetHeight(1)
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	return chatState{input: in}
}

func (c chatState) resize(width int) chatState {
	out := c
	out.input.SetWidth(width - 2)
	return out
}

// grow sets the composer height to its content, up to maxComposerLines.
func (c chatState) grow() chatState {
	out := c
	lines := strings.Count(out.input.Value(), "\n") + 1
	if lines > maxComposerLines {
		lines = maxComposerLines
	}
	out.input.SetHeight(lines)
	return out
}

func (c chatState) suggestions() []engine.BotView {
	q, ok := mentionQuery(c.input.Value())
	if !ok {
		return nil
	}
	return suggestions(c.bots, q)
}

func (m Model) openChat(id string) (tea.Model, tea.Cmd) {
	if m.stop != nil {
		m.stop()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	out := m
	out.stop = cancel
	out.view, out.focusMain = viewChat, true
	for i, it := range out.tree {
		if it.chatID == id {
			out.cursor = i // keep the tree selection on the open chat
		}
	}
	out.chat = newChatState().resize(out.mainWidth())
	out.chat.input.Focus()
	api := m.api
	load := func() tea.Msg {
		c, err := api.GetChat(ctx, id)
		if err != nil {
			return chatLoaded{err: err}
		}
		msgs, err := api.Messages(ctx, id, 200)
		if err != nil {
			return chatLoaded{err: err}
		}
		bots, err := api.ListBots(ctx, c.Scope)
		if err != nil {
			return chatLoaded{err: err}
		}
		var allowed []engine.BotView
		for _, b := range bots {
			if canJoin(b.Scope, c.Scope) {
				allowed = append(allowed, b)
			}
		}
		return chatLoaded{chat: c, msgs: msgs, bots: allowed}
	}
	subscribe := func() tea.Msg {
		ch, err := api.Stream(ctx, id)
		if err != nil {
			return resultMsg{err: err}
		}
		return waitStream(id, ch)()
	}
	return out, tea.Batch(load, subscribe)
}

func waitStream(chat string, ch <-chan store.Message) tea.Cmd {
	return func() tea.Msg {
		msg, open := <-ch
		if !open {
			return streamClosed{chat: chat}
		}
		return streamMsg{chat: chat, msg: msg, ch: ch}
	}
}

func (m Model) updateChatMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	out := m
	switch msg := msg.(type) {
	case chatLoaded:
		if msg.err != nil {
			return out.flash("", msg.err), nil
		}
		out.chat.chat, out.chat.msgs, out.chat.bots = msg.chat, msg.msgs, msg.bots
	case streamMsg:
		if msg.chat == out.chat.chat.ID || out.chat.chat.ID == "" {
			out.chat.msgs = appendMsg(out.chat.msgs, msg.msg)
		}
		return out, waitStream(msg.chat, msg.ch)
	}
	return out, nil
}

func (m Model) chatKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	out := m
	c := &out.chat
	if c.rerouting {
		return out.rerouteKey(k)
	}
	sugg := c.suggestions()
	empty := strings.TrimSpace(c.input.Value()) == ""
	switch s := k.String(); {
	case s == "esc":
		out.focusMain = false
		return out, nil
	case len(sugg) > 0 && (s == "up" || s == "down"):
		step := 1
		if s == "up" {
			step = -1
		}
		c.mention = (c.mention + step + len(sugg)) % len(sugg)
		return out, nil
	case len(sugg) > 0 && (s == "tab" || s == "enter"):
		c.input.SetValue(applyMention(c.input.Value(), sugg[min(c.mention, len(sugg)-1)].Handle))
		c.input.CursorEnd()
		c.mention = 0
		return out, nil
	case s == "enter":
		text := strings.TrimSpace(c.input.Value())
		if text == "" {
			return out, nil
		}
		c.input.Reset()
		out.chat = out.chat.grow()
		api, ctx, id := out.api, out.ctx, c.chat.ID
		return out, func() tea.Msg {
			_, err := api.Post(ctx, id, text)
			return resultMsg{err: err}
		}
	case empty && len(s) == 1 && s >= "1" && s <= "9":
		return out.pick(int(s[0] - '1'))
	case empty && s == "r":
		if _, ok := lastUserMessage(c.msgs); ok && len(c.bots) > 0 {
			c.rerouting, c.rerouteIdx = true, 0
		}
		return out, nil
	case empty && s == "m" && c.chat.Kind == store.KindGroup:
		next := store.RoutingMention
		if c.chat.Routing == store.RoutingMention {
			next = store.RoutingModerated
		}
		c.chat.Routing = next
		api, ctx, id := out.api, out.ctx, c.chat.ID
		return out, func() tea.Msg {
			return resultMsg{info: "routing: " + next, err: api.SetRouting(ctx, id, next)}
		}
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(k)
	c.mention = 0
	out.chat = out.chat.grow()
	return out, cmd
}

func (m Model) pick(i int) (tea.Model, tea.Cmd) {
	q, opts, ok := pendingQuestion(m.chat.msgs)
	if !ok || i >= len(opts) {
		return m, nil
	}
	api, ctx, id, bot := m.api, m.ctx, m.chat.chat.ID, opts[i]
	return m, func() tea.Msg {
		_, err := api.Pick(ctx, id, q.ID, bot)
		return resultMsg{info: "routed to " + bot, err: err}
	}
}

func (m Model) rerouteKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	out := m
	c := &out.chat
	switch k.String() {
	case "esc":
		c.rerouting = false
	case "up", "k":
		c.rerouteIdx = (c.rerouteIdx - 1 + len(c.bots)) % len(c.bots)
	case "down", "j":
		c.rerouteIdx = (c.rerouteIdx + 1) % len(c.bots)
	case "enter":
		c.rerouting = false
		target, ok := lastUserMessage(c.msgs)
		if !ok {
			return out, nil
		}
		api, ctx, id, bot := out.api, out.ctx, c.chat.ID, c.bots[c.rerouteIdx].Handle
		return out, func() tea.Msg {
			_, err := api.Reroute(ctx, id, target.ID, bot)
			return resultMsg{info: "rerouted to " + bot, err: err}
		}
	}
	return out, nil
}

func (m Model) chatView() string {
	c := m.chat
	w := m.mainWidth() - 2
	mode := ""
	if c.chat.Kind == store.KindGroup {
		mode = " · " + c.chat.Routing
	}
	header := titleStyle.Render(c.chat.ID + mode)
	var lines []string
	q, opts, hasQ := pendingQuestion(c.msgs)
	for _, msg := range c.msgs {
		lines = append(lines, renderMessage(msg, w, hasQ && msg.ID == q.ID, opts)...)
	}
	var bottom []string
	if c.rerouting {
		bottom = append(bottom, popup("Reroute last message to", c.bots, c.rerouteIdx, w))
	} else if sugg := c.suggestions(); len(sugg) > 0 {
		bottom = append(bottom, popup("", sugg, c.mention, w))
	}
	bottom = append(bottom, c.input.View())
	avail := m.bodyHeight() - 2 - lipgloss.Height(strings.Join(bottom, "\n"))
	if len(lines) > avail && avail > 0 {
		lines = lines[len(lines)-avail:]
	}
	body := strings.Join(lines, "\n")
	gap := avail - len(lines)
	if gap < 0 {
		gap = 0
	}
	return header + "\n" + body + strings.Repeat("\n", gap+1) + strings.Join(bottom, "\n")
}

func renderMessage(msg store.Message, width int, pending bool, opts []string) []string {
	ts := faintStyle.Render(msg.TS.Local().Format("15:04"))
	wrap := func(s string, indent int) []string {
		return strings.Split(lipgloss.NewStyle().Width(width-indent).Render(s), "\n")
	}
	switch msg.Kind {
	case store.MsgSystem:
		return []string{faintStyle.Render("      · " + msg.Text)}
	case store.MsgQuestion:
		out := []string{askStyle.Render("      ? " + strings.SplitN(msg.Text, " Options:", 2)[0])}
		if pending {
			var o []string
			for i, h := range opts {
				o = append(o, fmt.Sprintf("[%d] %s", i+1, h))
			}
			out = append(out, askStyle.Render("        "+strings.Join(o, "   ")))
		}
		return out
	}
	name := botStyle.Render(msg.From)
	if msg.From == "@user" {
		name = userStyle.Render("you")
	}
	if msg.Kind == store.MsgRefusal {
		name += warnStyle.Render(" (refused)")
	}
	body := wrap(msg.Text, 8)
	out := []string{ts + " " + name}
	for _, l := range body {
		out = append(out, "      "+l)
	}
	return out
}

func popup(title string, bots []engine.BotView, sel, width int) string {
	var rows []string
	if title != "" {
		rows = append(rows, mutedStyle.Render(title))
	}
	for i, b := range bots {
		note := strings.SplitN(b.Charter, "\n", 2)[0]
		if b.Status != "active" {
			note = b.Status + " · messages queue"
		}
		row := fmt.Sprintf("%-22s %s", b.Handle, note)
		if len(row) > width-6 && width > 10 {
			row = row[:width-7] + "…"
		}
		if i == sel {
			row = selStyle.Render("▸ " + row)
		} else {
			row = "  " + mutedStyle.Render(row)
		}
		rows = append(rows, row)
	}
	return popupStyle.Render(strings.Join(rows, "\n"))
}
