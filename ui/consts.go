package ui

import "github.com/charmbracelet/lipgloss"

// FallBackText is the big project logo shown in an empty preview.
var FallBackText = lipgloss.JoinVertical(lipgloss.Center, `
░██████╗░██████╗░░██████╗██╗░░██╗███████╗███████╗████████╗██████╗░░█████╗░
██╔═══██╗██╔══██╗██╔════╝██║░░██║██╔════╝██╔════╝╚══██╔══╝██╔══██╗██╔══██╗
██║░░░██║██████╔╝██║░░░░░███████║█████╗░░███████╗░░░██║░░░██████╔╝███████║
██║░░░██║██╔══██╗██║░░░░░██╔══██║██╔══╝░░╚════██║░░░██║░░░██╔══██╗██╔══██║
╚██████╔╝██║░░██║╚██████╗██║░░██║███████╗███████║░░░██║░░░██║░░██║██║░░██║
░╚═════╝░╚═╝░░╚═╝░╚═════╝╚═╝░░╚═╝╚══════╝╚══════╝░░░╚═╝░░░╚═╝░░╚═╝╚═╝░░╚═╝
`, lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b35c")).Render("░░ b y   V i t a l i k ░░"))

// FallBackTextCompact is the logo for narrow panes.
var FallBackTextCompact = lipgloss.JoinVertical(lipgloss.Center,
	lipgloss.NewStyle().Bold(true).Render("♪ orchestra")+lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e3b35c")).Render("ByVitalik"),
	"")

// FallbackLogo picks the logo that fits width.
func FallbackLogo(width int) string {
	if width > 0 && width < 80 {
		return FallBackTextCompact
	}
	return FallBackText
}
