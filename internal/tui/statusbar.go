package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders the bottom status bar.
type StatusBar struct {
	Model      string
	State      string
	AutoAccept bool
	Theme      Theme
	Width      int
}

// View renders the status bar.
func (s StatusBar) View() string {
	autoStr := "OFF"
	if s.AutoAccept {
		autoStr = "ON"
	}

	content := fmt.Sprintf(" %s | %s | Auto: %s | Ctrl+C quit | a: auto-accept ",
		s.Model, s.State, autoStr)

	style := lipgloss.NewStyle().
		Background(s.Theme.Accent).
		Foreground(s.Theme.Bg).
		Width(s.Width).
		Bold(true)

	return style.Render(content)
}
