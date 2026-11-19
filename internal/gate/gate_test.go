package gate

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantDec  Decision
		wantBody string
	}{
		{"act", "{\"decision\":\"act\"}\nHere is the review.", Decision{Decision: Act}, "Here is the review."},
		{"refuse", "{\"decision\":\"refuse\",\"reason\":\"not Go\"}\nTry @db.", Decision{Decision: Refuse, Reason: "not Go"}, "Try @db."},
		{"delegate", "{\"decision\":\"delegate\",\"target\":\"@global/ops\",\"reason\":\"infra\"}", Decision{Decision: Delegate, Target: "@global/ops", Reason: "infra"}, ""},
		{"delegate without target", "{\"decision\":\"delegate\",\"reason\":\"?\"}\nx", Decision{Decision: Refuse, Reason: "?"}, "x"},
		{"no decision line", "Just an answer.\nTwo lines.", Decision{Decision: Act}, "Just an answer.\nTwo lines."},
		{"unknown decision", "{\"decision\":\"maybe\"}\nhm", Decision{Decision: Act}, "{\"decision\":\"maybe\"}\nhm"},
		{"fenced", "```json\n{\"decision\":\"act\"}\n```\nBody", Decision{Decision: Act}, "Body"},
		{"inline backticks", "`{\"decision\":\"act\"}`\nBody", Decision{Decision: Act}, "Body"},
	}
	for _, c := range cases {
		d, body := Parse(c.in)
		if d != c.wantDec || body != c.wantBody {
			t.Errorf("%s: Parse = %+v %q; want %+v %q", c.name, d, body, c.wantDec, c.wantBody)
		}
	}
}
