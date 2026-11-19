package ids

import (
	"regexp"
	"testing"
)

func TestNewIsSortableAndUnique(t *testing.T) {
	seen := map[string]bool{}
	prev := ""
	for i := 0; i < 70000; i++ {
		id := New()
		if len(id) != 20 {
			t.Fatalf("len(%q) = %d, want 20", id, len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		if id[:16] <= prev {
			t.Fatalf("ids not strictly increasing: %q <= %q", id[:16], prev)
		}
		prev = id[:16]
		seen[id] = true
	}
}

func TestUUIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if u := UUID(); !re.MatchString(u) {
		t.Fatalf("UUID() = %q, not a v4 uuid", u)
	}
}

func TestTokenLength(t *testing.T) {
	if got := len(Token()); got != 64 {
		t.Fatalf("len(Token()) = %d, want 64", got)
	}
}
