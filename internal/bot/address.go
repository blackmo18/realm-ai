package bot

import (
	"fmt"
	"strings"
)

// UserHandle is the sender handle for the human user.
const UserHandle = "@user"

// Address identifies a bot. Scope is empty for an unqualified "@name".
type Address struct {
	Scope string
	Name  string
}

// ParseAddress parses "@name", "@global/name" or "@<project>/name".
func ParseAddress(s string) (Address, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(s), "@")
	if raw == "" {
		return Address{}, fmt.Errorf("empty bot address")
	}
	scope, name, qualified := strings.Cut(raw, "/")
	if !qualified {
		name, scope = scope, ""
	}
	if qualified && !scopeRe.MatchString(scope) {
		return Address{}, fmt.Errorf("invalid scope %q in address %q", scope, s)
	}
	if !nameRe.MatchString(name) {
		return Address{}, fmt.Errorf("invalid bot name %q in address %q", name, s)
	}
	return Address{Scope: scope, Name: name}, nil
}

// Qualified reports whether the address names its scope.
func (a Address) Qualified() bool { return a.Scope != "" }

// String renders the address with a leading @.
func (a Address) String() string {
	if a.Scope == "" {
		return "@" + a.Name
	}
	return "@" + a.Scope + "/" + a.Name
}
