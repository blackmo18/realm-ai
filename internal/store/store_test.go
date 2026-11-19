package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "x", "realm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestProjects(t *testing.T) {
	s := open(t)
	p, err := s.RegisterProject("app", "/code/app")
	if err != nil || p.Slug != "app" {
		t.Fatalf("register: %+v %v", p, err)
	}
	again, err := s.RegisterProject("other", "/code/app")
	if err != nil || again.Slug != "app" {
		t.Fatalf("re-register same root: %+v %v", again, err)
	}
	clash, err := s.RegisterProject("app", "/work/app")
	if err != nil || clash.Slug != "app-2" {
		t.Fatalf("slug clash: %+v %v", clash, err)
	}
	if got, err := s.ProjectBySlug("app-2"); err != nil || got.Root != "/work/app" {
		t.Fatalf("by slug: %+v %v", got, err)
	}
	if got, err := s.ProjectByRoot("/code/app"); err != nil || got.Slug != "app" {
		t.Fatalf("by root: %+v %v", got, err)
	}
	if _, err := s.ProjectBySlug("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := s.ProjectByRoot("/nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	all, err := s.Projects()
	if err != nil || len(all) != 2 {
		t.Fatalf("projects: %+v %v", all, err)
	}
}

func TestChatsAndMembers(t *testing.T) {
	s := open(t)
	c, err := s.CreateChat(Chat{ID: "app/dev", Kind: KindGroup, Scope: "app", Name: "dev", Routing: RoutingModerated})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChat(c); err == nil {
		t.Fatal("duplicate chat accepted")
	}
	got, err := s.GetChat("app/dev")
	if err != nil || got.Routing != RoutingModerated || got.Created.IsZero() {
		t.Fatalf("get chat: %+v %v", got, err)
	}
	if _, err := s.GetChat("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.SetRouting("app/dev", RoutingMention); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRouting("missing", RoutingMention); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	_, _ = s.CreateChat(Chat{ID: "dm/@a+@b", Kind: KindDM, Scope: "app", Name: "@a+@b", Routing: RoutingMention})
	groups, _ := s.ListChats(KindGroup)
	all, _ := s.ListChats("")
	if len(groups) != 1 || len(all) != 2 {
		t.Fatalf("list chats: groups=%d all=%d", len(groups), len(all))
	}
	for _, m := range []string{"@app/b", "@app/a", "@app/a"} {
		if err := s.AddMember("app/dev", m); err != nil {
			t.Fatal(err)
		}
	}
	members, _ := s.Members("app/dev")
	if len(members) != 2 || members[0] != "@app/a" {
		t.Fatalf("members: %v", members)
	}
	_ = s.RemoveMember("app/dev", "@app/a")
	members, _ = s.Members("app/dev")
	if len(members) != 1 {
		t.Fatalf("remove member: %v", members)
	}
}

func TestMessages(t *testing.T) {
	s := open(t)
	for i := 0; i < 5; i++ {
		m := Message{ID: fmt.Sprintf("m%d", i), Chat: "c", Thread: "m0", From: "@user", Kind: MsgText,
			Mentions: []string{"@x"}, Text: fmt.Sprintf("hello %d", i), TS: time.Now()}
		if err := s.InsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := s.Recent("c", 3)
	if err != nil || len(recent) != 3 || recent[0].ID != "m2" || recent[2].ID != "m4" {
		t.Fatalf("recent: %+v %v", recent, err)
	}
	m, err := s.GetMessage("m1")
	if err != nil || m.Text != "hello 1" || len(m.Mentions) != 1 || m.TS.IsZero() {
		t.Fatalf("get message: %+v %v", m, err)
	}
	if _, err := s.GetMessage("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSessionsInboxAsks(t *testing.T) {
	s := open(t)
	if id, err := s.Session("@a", "c"); err != nil || id != "" {
		t.Fatalf("empty session: %q %v", id, err)
	}
	_ = s.SetSession("@a", "c", "s1")
	_ = s.SetSession("@a", "c", "s2")
	if id, _ := s.Session("@a", "c"); id != "s2" {
		t.Fatalf("session = %q", id)
	}

	_ = s.PushInbox("@a", []byte("one"))
	_ = s.PushInbox("@a", []byte("two"))
	_ = s.PushInbox("@b", []byte("other"))
	items, err := s.DrainInbox("@a")
	if err != nil || len(items) != 2 || string(items[0]) != "one" {
		t.Fatalf("drain: %q %v", items, err)
	}
	if items, _ := s.DrainInbox("@a"); len(items) != 0 {
		t.Fatalf("drain not emptied: %q", items)
	}

	if _, err := s.LatestAsk("c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	_ = s.SaveAsk(Ask{MessageID: "q1", ChatID: "c", SourceID: "m1", Options: []string{"@a", "@b"}})
	_ = s.SaveAsk(Ask{MessageID: "q2", ChatID: "c", SourceID: "m2", Options: []string{"@a"}})
	a, err := s.LatestAsk("c")
	if err != nil || a.MessageID != "q2" || a.SourceID != "m2" {
		t.Fatalf("latest ask: %+v %v", a, err)
	}
	a, err = s.GetAsk("q1")
	if err != nil || len(a.Options) != 2 {
		t.Fatalf("get ask: %+v %v", a, err)
	}
	_ = s.DeleteAsk("q1")
	if _, err := s.GetAsk("q1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ask not deleted: %v", err)
	}
}
