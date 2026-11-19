// Package policy decides which actors may read or write which chats and
// who may message whom. It is pure and has no I/O.
//
// Rules:
//   - The user may do anything.
//   - A global bot may access any chat (global or any project) and DM anyone.
//   - A project bot may access only its own project's chats. It may DM bots
//     of its own project and global bots, never bots of other projects.
package policy

import "github.com/blackmo18/realm-ai/internal/config"

// Actor is the user or a bot in a scope.
type Actor struct {
	IsUser bool
	Scope  string
}

// User returns the human user actor.
func User() Actor { return Actor{IsUser: true} }

// Bot returns a bot actor in a scope.
func Bot(scope string) Actor { return Actor{Scope: scope} }

func (a Actor) isGlobal() bool { return !a.IsUser && a.Scope == config.GlobalScope }

// CanAccessChat reports whether a may read and post in a chat of chatScope.
func CanAccessChat(a Actor, chatScope string) bool {
	if a.IsUser || a.isGlobal() {
		return true
	}
	return a.Scope == chatScope
}

// CanDM reports whether from may send a direct message to to.
func CanDM(from, to Actor) bool {
	switch {
	case from.IsUser || to.IsUser:
		return true
	case from.isGlobal():
		return true
	case to.isGlobal():
		return true
	default:
		return from.Scope == to.Scope
	}
}

// DMScope returns the scope whose database stores a DM between a and b:
// a project scope if either side is a project bot, otherwise global.
func DMScope(a, b Actor) string {
	for _, x := range []Actor{a, b} {
		if !x.IsUser && x.Scope != config.GlobalScope {
			return x.Scope
		}
	}
	return config.GlobalScope
}
