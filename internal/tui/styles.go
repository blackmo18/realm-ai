package tui

import "github.com/charmbracelet/lipgloss"

var (
	colAccent = lipgloss.Color("#7DD3FC")
	colMuted  = lipgloss.Color("#A1A1AA")
	colFaint  = lipgloss.Color("#71717A")
	colAsk    = lipgloss.Color("#FACC15")
	colOK     = lipgloss.Color("#4ADE80")
	colWarn   = lipgloss.Color("#FDBA74")
	colBad    = lipgloss.Color("#F87171")
	colBot    = lipgloss.Color("#93C5FD")

	paneStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3F3F46")).Padding(0, 1)
	activePane  = paneStyle.BorderForeground(colAccent)
	titleStyle  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(colMuted)
	faintStyle  = lipgloss.NewStyle().Foreground(colFaint)
	askStyle    = lipgloss.NewStyle().Foreground(colAsk)
	okStyle     = lipgloss.NewStyle().Foreground(colOK)
	warnStyle   = lipgloss.NewStyle().Foreground(colWarn)
	badStyle    = lipgloss.NewStyle().Foreground(colBad)
	botStyle    = lipgloss.NewStyle().Foreground(colBot).Bold(true)
	userStyle   = lipgloss.NewStyle().Bold(true)
	selStyle    = lipgloss.NewStyle().Background(lipgloss.Color("#3B3B46")).Foreground(lipgloss.Color("#FFFFFF"))
	keyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	footerStyle = lipgloss.NewStyle().Foreground(colMuted).Background(lipgloss.Color("#27272F")).Padding(0, 1)
	popupStyle  = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#52525B")).Padding(0, 1)
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "active":
		return okStyle
	case "archived":
		return faintStyle
	default:
		return mutedStyle
	}
}

// footer renders "key label" pairs.
func footer(width int, pairs ...string) string {
	out := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		if out != "" {
			out += "   "
		}
		out += keyStyle.Render(pairs[i]) + " " + pairs[i+1]
	}
	return footerStyle.Width(width).Render(out)
}
