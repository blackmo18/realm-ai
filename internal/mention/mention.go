// Package mention extracts @handles from user text. It skips fenced code
// blocks, inline code and quoted lines so pasted content never triggers bots.
package mention

import (
	"regexp"
	"strings"
)

var (
	handleRe     = regexp.MustCompile(`(?:^|[\s(,;])@([a-z][a-z0-9-]*(?:/[a-z][a-z0-9-]*)?)`)
	inlineCodeRe = regexp.MustCompile("`[^`]*`")
)

// Parse returns unique handles ("@name" or "@scope/name") in order of first
// appearance.
func Parse(text string) []string {
	var out []string
	seen := map[string]bool{}
	inFence := false
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || strings.HasPrefix(trimmed, ">") {
			continue
		}
		clean := inlineCodeRe.ReplaceAllString(line, " ")
		for _, m := range handleRe.FindAllStringSubmatch(clean, -1) {
			h := "@" + m[1]
			if h == "@user" || seen[h] {
				continue
			}
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}
