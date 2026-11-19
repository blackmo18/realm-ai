package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/knowledge"
	"github.com/blackmo18/realm-ai/internal/store"
)

type fakeAPI struct {
	mu       sync.Mutex
	calls    []string
	msgs     []store.Message
	stream   chan store.Message
	bots     []engine.BotView
	routing  string
	failPost bool
}

func (f *fakeAPI) log(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeAPI) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (f *fakeAPI) Projects(context.Context) ([]store.Project, error) {
	return []store.Project{{Slug: "app", Root: "/code/app"}}, nil
}
func (f *fakeAPI) ListChats(_ context.Context, scope string) ([]store.Chat, error) {
	if scope == "app" {
		return []store.Chat{{ID: "app/dev", Kind: store.KindGroup, Scope: "app", Name: "dev"}, {ID: "app/dm:@app/db+@user", Kind: store.KindDM, Scope: "app", Name: "@app/db+@user"}}, nil
	}
	return nil, nil
}
func (f *fakeAPI) GetChat(_ context.Context, id string) (store.Chat, error) {
	return store.Chat{ID: id, Kind: store.KindGroup, Scope: "app", Name: "dev", Routing: f.routing}, nil
}
func (f *fakeAPI) Messages(context.Context, string, int) ([]store.Message, error) { return f.msgs, nil }
func (f *fakeAPI) Stream(context.Context, string) (<-chan store.Message, error)   { return f.stream, nil }
func (f *fakeAPI) Post(_ context.Context, chat, text string) (store.Message, error) {
	f.log("post " + chat + " " + text)
	if f.failPost {
		return store.Message{}, errors.New("daemon down")
	}
	return store.Message{}, nil
}
func (f *fakeAPI) Pick(_ context.Context, _, ask, bot string) (string, error) {
	f.log("pick " + ask + " " + bot)
	return bot, nil
}
func (f *fakeAPI) Reroute(_ context.Context, _, msg, bot string) (string, error) {
	f.log("reroute " + msg + " " + bot)
	return bot, nil
}
func (f *fakeAPI) SetRouting(_ context.Context, _, routing string) error {
	f.log("routing " + routing)
	return nil
}
func (f *fakeAPI) CreateChat(_ context.Context, in daemon.ChatCreate) (store.Chat, error) {
	f.log("chat " + in.Scope + "/" + in.Name + " " + in.Routing + " " + strings.Join(in.Members, ","))
	return store.Chat{ID: in.Scope + "/" + in.Name}, nil
}
func (f *fakeAPI) ListBots(context.Context, string) ([]engine.BotView, error) { return f.bots, nil }
func (f *fakeAPI) CreateBot(_ context.Context, in engine.BotInput) (engine.BotView, error) {
	f.log("bot " + in.Scope + "/" + in.Name + " tools=" + strings.Join(in.Tools, ","))
	return engine.BotView{Handle: "@" + in.Scope + "/" + in.Name}, nil
}
func (f *fakeAPI) SetBotStatus(_ context.Context, h, a string) (daemon.StatusResult, error) {
	f.log("status " + h + " " + a)
	return daemon.StatusResult{Bot: engine.BotView{Handle: h, Status: "off"}}, nil
}
func (f *fakeAPI) InheritKnowledge(_ context.Context, h, from string) (int, error) {
	f.log("inherit " + h + " " + from)
	return 2, nil
}
func (f *fakeAPI) Knowledge(context.Context, string) ([]knowledge.Entry, error) {
	return []knowledge.Entry{{ID: "n1", Title: "Style", Body: "gofmt", Origin: "@global/old"}}, nil
}
func (f *fakeAPI) DM(_ context.Context, to, _, text string) (store.Message, error) {
	f.log("dm " + to + " " + text)
	return store.Message{Chat: "app/dm:@app/db+@user"}, nil
}
func (f *fakeAPI) Templates(context.Context) ([]engine.Template, error) {
	return []engine.Template{{Name: "reviewer"}, {Name: "docs"}}, nil
}
func (f *fakeAPI) InspectFolder(_ context.Context, path string) (engine.FolderInfo, error) {
	return engine.FolderInfo{Path: path, Exists: true, IsGit: true, Slug: "shop"}, nil
}
func (f *fakeAPI) Onboard(_ context.Context, in engine.OnboardInput) (engine.OnboardResult, error) {
	f.log("onboard " + in.Dir + " starters=" + strings.Join(in.Starters, ",") + " ignore=" + map[bool]string{true: "y", false: "n"}[in.IgnoreDB])
	return engine.OnboardResult{Project: "shop", Chat: store.Chat{ID: "shop/general"}}, nil
}

func newFake() *fakeAPI {
	return &fakeAPI{
		stream:  make(chan store.Message, 4),
		routing: store.RoutingModerated,
		bots: []engine.BotView{
			{Handle: "@app/db", Name: "db", Scope: "app", Status: "active", Charter: "Owns schema", Role: "bot"},
			{Handle: "@app/docs", Name: "docs", Scope: "app", Status: "off", Charter: "Docs", Role: "bot"},
			{Handle: "@global/ops", Name: "ops", Scope: "global", Status: "active", Charter: "Deploys", Role: "bot"},
			{Handle: "@app/moderator", Name: "moderator", Scope: "app", Status: "active", Role: "moderator"},
		},
	}
}

// run feeds a message through Update and executes returned commands
// (one level of batching) so async results land in the model.
func run(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	out := next.(Model)
	return drain(t, out, cmd, 0)
}

func drain(t *testing.T, m Model, cmd tea.Cmd, depth int) Model {
	if cmd == nil || depth > 3 {
		return m
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(200 * time.Millisecond):
		return m // a stream wait with nothing to deliver
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = drain(t, m, c, depth+1)
		}
		return m
	}
	next, cmd2 := m.Update(msg)
	return drain(t, next.(Model), cmd2, depth+1)
}

func press(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeText(t *testing.T, m Model, s string) Model {
	for _, r := range s {
		m = run(t, m, press(string(r)))
	}
	return m
}

func start(t *testing.T, f *fakeAPI) Model {
	t.Helper()
	m := New(context.Background(), f, "app")
	m = run(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	return drain(t, m, m.Init(), 0)
}

func TestTreeAndOpenChat(t *testing.T) {
	f := newFake()
	m := start(t, f)
	labels := []string{}
	for _, it := range m.tree {
		labels = append(labels, it.label)
	}
	joined := strings.Join(labels, "|")
	for _, want := range []string{"global", "app", "# dev", "bots", "dm @app/db", "+ new chat", "+ add project…"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tree missing %q: %s", want, joined)
		}
	}
	for m.tree[m.cursor].label != "# dev" {
		m = run(t, m, press("down"))
	}
	f.msgs = []store.Message{{ID: "01", From: "@user", Kind: store.MsgText, Text: "hello", TS: time.Now()}}
	m = run(t, m, press("enter"))
	if m.view != viewChat || m.chat.chat.ID != "app/dev" || len(m.chat.msgs) != 1 {
		t.Fatalf("chat not opened: view=%v chat=%+v", m.view, m.chat.chat)
	}
	for _, b := range m.chat.bots {
		if b.Scope != "app" && b.Scope != "global" {
			t.Fatalf("bot from another scope: %+v", b)
		}
	}
	if !strings.Contains(m.View(), "app/dev · moderated") {
		t.Fatalf("view missing header:\n%s", m.View())
	}
	live := make(chan store.Message, 1)
	live <- store.Message{ID: "02", From: "@app/db", Kind: store.MsgReply, Text: "streamed", TS: time.Now()}
	m = drain(t, m, waitStream("app/dev", live), 0)
	if len(m.chat.msgs) != 2 {
		t.Fatalf("stream message not appended: %d", len(m.chat.msgs))
	}
}

func openDev(t *testing.T, f *fakeAPI) Model {
	m := start(t, f)
	next, cmd := m.openChat("app/dev")
	return drain(t, next.(Model), cmd, 0)
}

func TestComposerMentionSendAndGrow(t *testing.T) {
	f := newFake()
	m := openDev(t, f)
	m = typeText(t, m, "ask @d")
	if s := m.chat.suggestions(); len(s) != 2 || s[0].Handle != "@app/db" {
		t.Fatalf("suggestions: %+v", s)
	}
	if !strings.Contains(m.View(), "▸ @app/db") {
		t.Fatalf("popup not rendered above composer:\n%s", m.View())
	}
	m = run(t, m, press("enter"))
	if m.chat.input.Value() != "ask @app/db " {
		t.Fatalf("mention not applied: %q", m.chat.input.Value())
	}
	m = run(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = typeText(t, m, "line two")
	if m.chat.input.Height() != 2 {
		t.Fatalf("composer should grow to 2 lines, got %d", m.chat.input.Height())
	}
	m = run(t, m, press("enter"))
	if !f.called("post app/dev ask @app/db \nline two") || m.chat.input.Value() != "" {
		t.Fatalf("post not sent: %v / %q", f.calls, m.chat.input.Value())
	}
	f.failPost = true
	m = typeText(t, m, "x")
	m = run(t, m, press("enter"))
	if !m.isError || !strings.Contains(m.status, "daemon down") {
		t.Fatalf("post error not shown: %q", m.status)
	}
}

func TestPickRerouteRouting(t *testing.T) {
	f := newFake()
	f.msgs = []store.Message{
		{ID: "01", From: "@user", Kind: store.MsgText, Text: "look at users"},
		{ID: "02", Parent: "01", From: "@app/moderator", Kind: store.MsgQuestion, Text: "Code or schema? Options: @app/reviewer, @app/db. Reply with one handle."},
	}
	m := openDev(t, f)
	if !strings.Contains(m.View(), "[2] @app/db") {
		t.Fatalf("question options not shown:\n%s", m.View())
	}
	m = run(t, m, press("2"))
	if !f.called("pick 02 @app/db") {
		t.Fatalf("pick not sent: %v", f.calls)
	}
	m = run(t, m, press("r"))
	if !m.chat.rerouting {
		t.Fatal("reroute picker not open")
	}
	m = run(t, m, press("down"))
	m = run(t, m, press("enter"))
	if !f.called("reroute 01 ") {
		t.Fatalf("reroute not sent: %v", f.calls)
	}
	m = run(t, m, press("m"))
	if !f.called("routing mention") || m.chat.chat.Routing != store.RoutingMention {
		t.Fatalf("routing toggle: %v", f.calls)
	}
	m = run(t, m, press("esc"))
	if m.focusMain {
		t.Fatal("esc should return focus to the tree")
	}
}

func TestNewChatForm(t *testing.T) {
	f := newFake()
	m := openDev(t, f)
	m = run(t, m, press("esc"))
	m = run(t, m, press("n"))
	if m.view != viewForm {
		t.Fatal("form not shown")
	}
	m = typeText(t, m, "release")
	m = run(t, m, press("ctrl+s"))
	if !f.called("chat app/release moderated") || m.view != viewChat {
		t.Fatalf("chat not created/opened: %v view=%v", f.calls, m.view)
	}
}

func TestBotFormWarnsAndCreates(t *testing.T) {
	f := newFake()
	m := start(t, f)
	next, cmd := m.openBots("app")
	m = drain(t, next.(Model), cmd, 0)
	if len(m.bots.list) != 3 {
		t.Fatalf("moderator should be hidden: %+v", m.bots.list)
	}
	m = run(t, m, press("a"))
	m = typeText(t, m, "tester")
	for i := 0; i < 7; i++ {
		m = run(t, m, press("tab"))
	}
	for i := 0; i < 7; i++ {
		m = run(t, m, press("right"))
	}
	m = run(t, m, press("space"))
	if !strings.Contains(m.View(), "Bash runs as your user") {
		t.Fatalf("risky tool warning missing:\n%s", m.View())
	}
	m = run(t, m, press("ctrl+s"))
	if !f.called("bot app/tester tools=Read,Grep,Glob,Bash") {
		t.Fatalf("bot not created: %v", f.calls)
	}
	if m.view != viewBots {
		t.Fatalf("should return to bots, view=%v", m.view)
	}
}

func TestBotsActions(t *testing.T) {
	f := newFake()
	m := start(t, f)
	next, cmd := m.openBots("app")
	m = drain(t, next.(Model), cmd, 0)
	m = run(t, m, press("o"))
	if !f.called("status @app/db off") {
		t.Fatalf("toggle: %v", f.calls)
	}
	m = run(t, m, press("x"))
	m = run(t, m, press("n"))
	if f.called("status @app/db archive") {
		t.Fatal("archive without confirmation")
	}
	m = run(t, m, press("x"))
	m = run(t, m, press("y"))
	if !f.called("status @app/db archive") {
		t.Fatalf("archive: %v", f.calls)
	}
	m = run(t, m, press("enter"))
	if !m.bots.showKnow || !strings.Contains(m.View(), "(from @global/old)") {
		t.Fatalf("knowledge not shown:\n%s", m.View())
	}
	m = run(t, m, press("i"))
	m = run(t, m, press("ctrl+s"))
	if !f.called("inherit @app/db @app/docs") {
		t.Fatalf("inherit: %v", f.calls)
	}
	m = run(t, m, press("d"))
	m = typeText(t, m, "hi")
	m = run(t, m, press("ctrl+s"))
	if !f.called("dm @app/db hi") || m.view != viewChat {
		t.Fatalf("dm: %v view=%v", f.calls, m.view)
	}
}

func TestProjectForm(t *testing.T) {
	f := newFake()
	m := start(t, f)
	m = run(t, m, press("p"))
	m = run(t, m, press("ctrl+s"))
	if m.form.err != "project folder is required" {
		t.Fatalf("missing folder error: %q", m.form.err)
	}
	m = typeText(t, m, "~/code/shop")
	if !strings.Contains(m.View(), "will be @shop") {
		t.Fatalf("folder inspection not shown:\n%s", m.View())
	}
	m = run(t, m, press("ctrl+s"))
	if !f.called("onboard ~/code/shop starters=reviewer ignore=y") || m.chat.chat.ID != "shop/general" {
		t.Fatalf("onboard: %v chat=%s", f.calls, m.chat.chat.ID)
	}
}

func TestQuitAndHelpers(t *testing.T) {
	m := start(t, newFake())
	if _, cmd := m.Update(press("ctrl+c")); cmd == nil {
		t.Fatal("ctrl+c should quit")
	}
	if q, ok := mentionQuery("mail a@b"); ok {
		t.Fatalf("email treated as mention: %q", q)
	}
	if got := splitList(" a, ,b "); strings.Join(got, "|") != "a|b" {
		t.Fatalf("splitList: %v", got)
	}
	if _, _, ok := pendingQuestion(nil); ok {
		t.Fatal("no question expected")
	}
	if !strings.Contains(New(context.Background(), newFake(), "").View(), "loading") {
		t.Fatal("zero-size view should say loading")
	}
}

func TestStaleChatResultsIgnored(t *testing.T) {
	f := newFake()
	m := openDev(t, f)
	next, _ := m.openChat("app/other")
	m = next.(Model)
	m = run(t, m, chatLoaded{id: "app/dev", chat: store.Chat{ID: "app/dev"}})
	if m.chat.chat.ID != "app/other" {
		t.Fatalf("late result switched chat to %q", m.chat.chat.ID)
	}
	m = run(t, m, streamMsg{chat: "app/dev", msg: store.Message{ID: "x"}, ch: make(chan store.Message)})
	if len(m.chat.msgs) != 0 {
		t.Fatal("old chat's stream leaked into the new chat")
	}
	m = run(t, m, streamMsg{chat: "app/other", msg: store.Message{ID: "05"}, ch: make(chan store.Message)})
	m = run(t, m, chatLoaded{id: "app/other", chat: store.Chat{ID: "app/other"}, msgs: []store.Message{{ID: "04"}}})
	if len(m.chat.msgs) != 2 || m.chat.msgs[0].ID != "04" {
		t.Fatalf("history should merge with streamed messages: %+v", m.chat.msgs)
	}
}
