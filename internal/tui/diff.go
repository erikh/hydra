package tui

import (
	"fmt"
	"strings"
)

// ComputeUnifiedDiff creates a simple unified diff between old and new content.
func ComputeUnifiedDiff(path, oldContent, newContent string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var buf strings.Builder
	fmt.Fprintf(&buf, "--- a/%s\n", path)
	fmt.Fprintf(&buf, "+++ b/%s\n", path)

	i, j := 0, 0
	for i < len(oldLines) || j < len(newLines) {
		switch {
		case i < len(oldLines) && j < len(newLines) && oldLines[i] == newLines[j]:
			fmt.Fprintf(&buf, " %s\n", oldLines[i])
			i++
			j++
		case i < len(oldLines):
			fmt.Fprintf(&buf, "-%s\n", oldLines[i])
			i++
		default:
			fmt.Fprintf(&buf, "+%s\n", newLines[j])
			j++
		}
	}

	return buf.String()
}

// RenderDiff colorizes a unified diff string using the theme.
func RenderDiff(diff string, theme Theme) string {
	if diff == "" {
		return ""
	}

	lines := strings.Split(diff, "\n")
	var rendered []string

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			rendered = append(rendered, theme.DiffHeaderStyle().Render(line))
		case strings.HasPrefix(line, "@@"):
			rendered = append(rendered, theme.AccentStyle().Render(line))
		case strings.HasPrefix(line, "+"):
			rendered = append(rendered, theme.DiffAddStyle().Render(line))
		case strings.HasPrefix(line, "-"):
			rendered = append(rendered, theme.DiffRemoveStyle().Render(line))
		default:
			rendered = append(rendered, theme.MutedStyle().Render(line))
		}
	}

	return strings.Join(rendered, "\n")
}
