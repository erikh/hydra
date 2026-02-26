package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/erikh/hydra/internal/claude"
)

// ApprovalDialog renders a tool approval prompt.
type ApprovalDialog struct {
	Request  claude.EventToolRequest
	Selected int // 0 = Accept, 1 = Reject
	Theme    Theme
	Width    int
}

// View renders the approval dialog.
func (a ApprovalDialog) View() string {
	var b strings.Builder

	headerStyle := a.Theme.AccentStyle().Bold(true)

	switch a.Request.Meta.Kind {
	case claude.ToolKindWrite, claude.ToolKindEdit:
		fmt.Fprintf(&b, "%s %s\n\n", headerStyle.Render("Tool:"), a.Request.Name)
		fmt.Fprintf(&b, "%s %s\n\n", headerStyle.Render("File:"), a.Request.Meta.Path)
		if a.Request.Meta.Diff != "" {
			fmt.Fprintf(&b, "%s\n", headerStyle.Render("Diff:"))
			b.WriteString(RenderDiff(a.Request.Meta.Diff, a.Theme))
			b.WriteString("\n")
		}
	case claude.ToolKindBash:
		fmt.Fprintf(&b, "%s %s\n\n", headerStyle.Render("Tool:"), a.Request.Name)
		fmt.Fprintf(&b, "%s\n", headerStyle.Render("Command:"))
		cmdStyle := lipgloss.NewStyle().
			Foreground(a.Theme.Warning).
			PaddingLeft(2)
		b.WriteString(cmdStyle.Render(a.Request.Meta.Command))
		b.WriteString("\n\n")
	default:
		fmt.Fprintf(&b, "%s %s\n", headerStyle.Render("Tool:"), a.Request.Name)
		if a.Request.Meta.Path != "" {
			fmt.Fprintf(&b, "%s %s\n", headerStyle.Render("Path:"), a.Request.Meta.Path)
		}
		b.WriteString("\n")
	}

	// Buttons.
	acceptStyle := lipgloss.NewStyle().
		Padding(0, 2).
		MarginRight(2)
	rejectStyle := lipgloss.NewStyle().
		Padding(0, 2)

	if a.Selected == 0 {
		acceptStyle = acceptStyle.
			Background(a.Theme.Success).
			Foreground(a.Theme.Bg).
			Bold(true)
		rejectStyle = rejectStyle.
			Foreground(a.Theme.Muted)
	} else {
		acceptStyle = acceptStyle.
			Foreground(a.Theme.Muted)
		rejectStyle = rejectStyle.
			Background(a.Theme.Error).
			Foreground(a.Theme.Bg).
			Bold(true)
	}

	buttons := lipgloss.JoinHorizontal(lipgloss.Center,
		acceptStyle.Render("Accept"),
		rejectStyle.Render("Reject"),
	)

	b.WriteString(buttons)

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(a.Theme.Accent).
		Padding(1, 2).
		Width(a.Width - 4)

	return border.Render(b.String())
}
