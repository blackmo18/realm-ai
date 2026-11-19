package policy

import "testing"

func TestCanAccessChat(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		chat  string
		want  bool
	}{
		{"user global chat", User(), "global", true},
		{"user project chat", User(), "app", true},
		{"global bot global chat", Bot("global"), "global", true},
		{"global bot project chat", Bot("global"), "app", true},
		{"project bot own chat", Bot("app"), "app", true},
		{"project bot global chat", Bot("app"), "global", false},
		{"project bot other project", Bot("app"), "web", false},
	}
	for _, c := range cases {
		if got := CanAccessChat(c.actor, c.chat); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCanDM(t *testing.T) {
	cases := []struct {
		name     string
		from, to Actor
		want     bool
	}{
		{"user to project bot", User(), Bot("app"), true},
		{"project bot to user", Bot("app"), User(), true},
		{"global to project", Bot("global"), Bot("app"), true},
		{"project to global", Bot("app"), Bot("global"), true},
		{"same project", Bot("app"), Bot("app"), true},
		{"other project", Bot("app"), Bot("web"), false},
		{"global to global", Bot("global"), Bot("global"), true},
	}
	for _, c := range cases {
		if got := CanDM(c.from, c.to); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDMScope(t *testing.T) {
	if s := DMScope(User(), Bot("app")); s != "app" {
		t.Errorf("user+project = %q", s)
	}
	if s := DMScope(Bot("global"), Bot("app")); s != "app" {
		t.Errorf("global+project = %q", s)
	}
	if s := DMScope(User(), Bot("global")); s != "global" {
		t.Errorf("user+global = %q", s)
	}
}
