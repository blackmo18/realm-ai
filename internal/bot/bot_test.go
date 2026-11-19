package bot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/blackmo18/realm-ai/internal/config"
)

func newRepo(t *testing.T) *Repo {
	t.Helper()
	return NewRepo(config.Scope{Name: "app", Root: filepath.Join(t.TempDir(), ".realm")})
}

func TestParseAddress(t *testing.T) {
	cases := []struct {
		in      string
		want    Address
		wantErr bool
	}{
		{in: "@helper", want: Address{Name: "helper"}},
		{in: "helper", want: Address{Name: "helper"}},
		{in: "@global/helper", want: Address{Scope: "global", Name: "helper"}},
		{in: "@realm-ai/reviewer", want: Address{Scope: "realm-ai", Name: "reviewer"}},
		{in: "@", wantErr: true},
		{in: "@Bad", wantErr: true},
		{in: "@x/../y", wantErr: true},
		{in: "@9x/y", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseAddress(c.in)
		if (err != nil) != c.wantErr {
			t.Fatalf("ParseAddress(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
		}
		if err == nil && got != c.want {
			t.Fatalf("ParseAddress(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
	if s := (Address{Scope: "global", Name: "x"}).String(); s != "@global/x" {
		t.Fatalf("String() = %q", s)
	}
	if s := (Address{Name: "x"}).String(); s != "@x" {
		t.Fatalf("String() = %q", s)
	}
}

func TestTransition(t *testing.T) {
	cases := []struct {
		from    Status
		action  string
		want    Status
		wantErr bool
	}{
		{StatusActive, ActionOff, StatusOff, false},
		{StatusOff, ActionOn, StatusActive, false},
		{StatusActive, ActionArchive, StatusArchived, false},
		{StatusOff, ActionArchive, StatusArchived, false},
		{StatusArchived, ActionRestore, StatusActive, false},
		{StatusArchived, ActionOn, StatusArchived, true},
		{StatusArchived, ActionOff, StatusArchived, true},
		{StatusActive, ActionRestore, StatusActive, true},
		{StatusActive, "explode", StatusActive, true},
	}
	for _, c := range cases {
		got, err := Transition(c.from, c.action)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("Transition(%s,%s) = %s,%v; want %s, err=%v", c.from, c.action, got, err, c.want, c.wantErr)
		}
	}
}

func TestValidate(t *testing.T) {
	base := Bot{Name: "helper", Charter: "Helps."}.WithDefaults()
	if err := base.Validate(); err != nil {
		t.Fatalf("valid bot rejected: %v", err)
	}
	bad := map[string]Bot{
		"name":     func() Bot { b := base; b.Name = "Bad Name"; return b }(),
		"reserved": func() Bot { b := base; b.Name = ModeratorName; return b }(),
		"role":     func() Bot { b := base; b.Role = "boss"; return b }(),
		"status":   func() Bot { b := base; b.Status = "sleepy"; return b }(),
		"effort":   func() Bot { b := base; b.Effort = "extreme"; return b }(),
		"charter":  func() Bot { b := base; b.Charter = "  "; return b }(),
		"provider": func() Bot { b := base; b.Provider = ""; return b }(),
	}
	for name, b := range bad {
		if err := b.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	mod := Bot{Name: ModeratorName, Role: RoleModerator, Charter: "Routes."}.WithDefaults()
	if err := mod.Validate(); err != nil {
		t.Fatalf("moderator rejected: %v", err)
	}
}

func TestRepoLifecycle(t *testing.T) {
	r := newRepo(t)
	b, err := r.Create(Bot{Name: "reviewer", Charter: "Reviews Go code.\nNothing else.", Model: "sonnet", Topics: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Handle() != "@app/reviewer" || b.Status != StatusActive || b.Provider != ProviderClaudeCLI {
		t.Fatalf("unexpected bot: %+v", b)
	}
	for _, d := range []string{KnowledgeDir(b), WorkspaceDir(b)} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("missing dir %s: %v", d, err)
		}
	}
	if _, err := r.Create(Bot{Name: "reviewer", Charter: "x"}); err == nil {
		t.Fatal("duplicate create succeeded")
	}
	off, err := r.Apply("reviewer", ActionOff)
	if err != nil || off.Status != StatusOff {
		t.Fatalf("off: %+v %v", off, err)
	}
	got, err := r.Get("reviewer")
	if err != nil || got.Status != StatusOff || got.FirstLine() != "Reviews Go code." {
		t.Fatalf("reload: %+v %v", got, err)
	}
	if _, err := r.Apply("reviewer", ActionArchive); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply("reviewer", ActionOn); err == nil {
		t.Fatal("turning on an archived bot should fail")
	}
	updated, err := r.Update("reviewer", func(b Bot) Bot { b.Model = "opus"; b.Name = "hijack"; return b })
	if err != nil || updated.Model != "opus" || updated.Name != "reviewer" {
		t.Fatalf("update: %+v %v", updated, err)
	}
}

func TestRepoListAndNotFound(t *testing.T) {
	r := newRepo(t)
	if bots, err := r.List(); err != nil || len(bots) != 0 {
		t.Fatalf("empty list: %v %v", bots, err)
	}
	for _, n := range []string{"zed", "alpha"} {
		if _, err := r.Create(Bot{Name: n, Charter: "c"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(r.Scope().BotsDir(), "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	bots, err := r.List()
	if err != nil || len(bots) != 2 || bots[0].Name != "alpha" {
		t.Fatalf("list: %+v %v", bots, err)
	}
	if _, err := r.Get("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := r.Apply("ghost", ActionOn); err == nil {
		t.Fatal("apply on missing bot succeeded")
	}
}
