package mention

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "please review the auth module", nil},
		{"single", "@reviewer check this", []string{"@reviewer"}},
		{"qualified", "ask @global/helper and @app/db-bot", []string{"@global/helper", "@app/db-bot"}},
		{"dedupe", "@a hi @a again", []string{"@a"}},
		{"email ignored", "mail me at bob@example.com", nil},
		{"inline code ignored", "run `@reviewer` literally, then @ops", []string{"@ops"}},
		{"fenced code ignored", "look:\n```\n@reviewer do x\n```\nthanks", nil},
		{"tilde fence ignored", "~~~\n@x\n~~~\n@y", []string{"@y"}},
		{"quote ignored", "> @reviewer said hi\nok @ops", []string{"@ops"}},
		{"punctuation prefix", "(@a), @b; @c", []string{"@a", "@b", "@c"}},
		{"user not a bot", "@user here", nil},
		{"uppercase not a handle", "@Reviewer", nil},
	}
	for _, c := range cases {
		if got := Parse(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Parse(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}
