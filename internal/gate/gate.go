// Package gate parses the charter decision line every bot reply starts with:
//
//	{"decision":"act"}
//	{"decision":"refuse","reason":"..."}
//	{"decision":"delegate","target":"@bot","reason":"..."}
package gate

import (
	"encoding/json"
	"strings"
)

// Decisions.
const (
	Act      = "act"
	Refuse   = "refuse"
	Delegate = "delegate"
)

// Decision is a bot's scope decision for one turn.
type Decision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Target   string `json:"target,omitempty"`
}

// Parse splits a reply into its decision and the remaining body. A reply
// without a valid decision line is treated as "act" with the full text.
func Parse(reply string) (Decision, string) {
	text := strings.TrimSpace(reply)
	text = strings.TrimPrefix(text, "```json")
	first, rest, _ := strings.Cut(strings.TrimSpace(text), "\n")
	first = strings.Trim(strings.TrimSpace(first), "`")
	var d Decision
	if err := json.Unmarshal([]byte(first), &d); err != nil {
		return Decision{Decision: Act}, strings.TrimSpace(reply)
	}
	rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "```"))
	switch d.Decision {
	case Act, Refuse:
		return d, rest
	case Delegate:
		if strings.TrimSpace(d.Target) == "" {
			return Decision{Decision: Refuse, Reason: d.Reason}, rest
		}
		return d, rest
	default:
		return Decision{Decision: Act}, strings.TrimSpace(reply)
	}
}
