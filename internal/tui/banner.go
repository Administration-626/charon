package tui

import "github.com/charmbracelet/lipgloss"

// bannerHeight is the number of terminal rows the banner block occupies,
// including the blank line separating it from the list title.
const bannerHeight = 9

// bannerArt is the CHARON wordmark (ANSI Shadow figlet style).
const bannerArt = ` ██████╗██╗  ██╗ █████╗ ██████╗  ██████╗ ███╗   ██╗
██╔════╝██║  ██║██╔══██╗██╔══██╗██╔═══██╗████╗  ██║
██║     ███████║███████║██████╔╝██║   ██║██╔██╗ ██║
██║     ██╔══██║██╔══██║██╔══██╗██║   ██║██║╚██╗██║
╚██████╗██║  ██║██║  ██║██║  ██║╚██████╔╝██║ ╚████║
 ╚═════╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝`

var (
	bannerStyle  = lipgloss.NewStyle().Foreground(colorBrand).Bold(true)
	taglineStyle = lipgloss.NewStyle().Italic(true).PaddingLeft(1)
)

// banner returns the styled splash shown atop the tool-selection screen.
func banner(version string) string {
	tag := "⛴  ferry your AI tools between endpoints"
	if version != "" {
		tag += "  ·  " + version
	}
	return bannerStyle.Render(bannerArt) + "\n" +
		taglineStyle.Render(tag)
}
