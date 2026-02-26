// Package tui provides the Bubbletea TUI for interactive Claude sessions.
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/lipgloss"
	"go.yaml.in/yaml/v4"
)

// Theme holds the color scheme for the TUI.
type Theme struct {
	Bg        lipgloss.Color
	Fg        lipgloss.Color
	Accent    lipgloss.Color
	Success   lipgloss.Color
	Error     lipgloss.Color
	Warning   lipgloss.Color
	Muted     lipgloss.Color
	Highlight lipgloss.Color
}

// DefaultTheme returns the hardcoded fallback theme.
func DefaultTheme() Theme {
	return Theme{
		Bg:        lipgloss.Color("#1a1b26"),
		Fg:        lipgloss.Color("#c0caf5"),
		Accent:    lipgloss.Color("#7aa2f7"),
		Success:   lipgloss.Color("#9ece6a"),
		Error:     lipgloss.Color("#f7768e"),
		Warning:   lipgloss.Color("#e0af68"),
		Muted:     lipgloss.Color("#565f89"),
		Highlight: lipgloss.Color("#bb9af7"),
	}
}

// globalColors holds optional color overrides from ~/.hydra.yml.
type globalColors struct {
	Bg        string `yaml:"bg"`
	Fg        string `yaml:"fg"`
	Accent    string `yaml:"accent"`
	Success   string `yaml:"success"`
	Error     string `yaml:"error"`
	Warning   string `yaml:"warning"`
	Muted     string `yaml:"muted"`
	Highlight string `yaml:"highlight"`
}

// globalConfig is the top-level structure of ~/.hydra.yml.
type globalConfig struct {
	Colors globalColors `yaml:"colors"`
}

// pywalColors is the JSON structure of ~/.cache/wal/colors.json.
type pywalColors struct {
	Special struct {
		Background string `json:"background"`
		Foreground string `json:"foreground"`
	} `json:"special"`
	Colors map[string]string `json:"colors"`
}

// LoadTheme loads colors with the following priority (highest to lowest):
//  1. ~/.hydra.yml colors (explicit user override)
//  2. pywal ~/.cache/wal/colors.json
//  3. DefaultTheme() hardcoded values
func LoadTheme() Theme {
	theme := loadPywalTheme()
	applyGlobalConfig(&theme)
	return theme
}

// loadPywalTheme loads colors from pywal if available, otherwise returns the default.
func loadPywalTheme() Theme {
	home, err := os.UserHomeDir()
	if err != nil {
		return DefaultTheme()
	}

	data, err := os.ReadFile(filepath.Join(home, ".cache", "wal", "colors.json")) //nolint:gosec // well-known pywal path
	if err != nil {
		return DefaultTheme()
	}

	var wal pywalColors
	if err := json.Unmarshal(data, &wal); err != nil {
		return DefaultTheme()
	}

	get := func(key, fallback string) lipgloss.Color {
		if v, ok := wal.Colors[key]; ok && v != "" {
			return lipgloss.Color(v)
		}
		return lipgloss.Color(fallback)
	}

	bg := lipgloss.Color(wal.Special.Background)
	if wal.Special.Background == "" {
		bg = DefaultTheme().Bg
	}
	fg := lipgloss.Color(wal.Special.Foreground)
	if wal.Special.Foreground == "" {
		fg = DefaultTheme().Fg
	}

	return Theme{
		Bg:        bg,
		Fg:        fg,
		Accent:    get("color4", "#7aa2f7"),
		Success:   get("color2", "#9ece6a"),
		Error:     get("color1", "#f7768e"),
		Warning:   get("color3", "#e0af68"),
		Muted:     get("color8", "#565f89"),
		Highlight: get("color5", "#bb9af7"),
	}
}

// applyGlobalConfig loads ~/.hydra.yml and overrides any color fields that are set.
func applyGlobalConfig(theme *Theme) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	data, err := os.ReadFile(filepath.Join(home, ".hydra.yml")) //nolint:gosec // well-known user config path
	if err != nil {
		return
	}

	var cfg globalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return
	}

	c := cfg.Colors
	if c.Bg != "" {
		theme.Bg = lipgloss.Color(c.Bg)
	}
	if c.Fg != "" {
		theme.Fg = lipgloss.Color(c.Fg)
	}
	if c.Accent != "" {
		theme.Accent = lipgloss.Color(c.Accent)
	}
	if c.Success != "" {
		theme.Success = lipgloss.Color(c.Success)
	}
	if c.Error != "" {
		theme.Error = lipgloss.Color(c.Error)
	}
	if c.Warning != "" {
		theme.Warning = lipgloss.Color(c.Warning)
	}
	if c.Muted != "" {
		theme.Muted = lipgloss.Color(c.Muted)
	}
	if c.Highlight != "" {
		theme.Highlight = lipgloss.Color(c.Highlight)
	}
}

// Derived styles.

// TextStyle returns the base text style.
func (t Theme) TextStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Fg)
}

// AccentStyle returns a style for accented text.
func (t Theme) AccentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
}

// ErrorStyle returns a style for error text.
func (t Theme) ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Error).Bold(true)
}

// SuccessStyle returns a style for success text.
func (t Theme) SuccessStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Success)
}

// WarningStyle returns a style for warning text.
func (t Theme) WarningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Warning)
}

// MutedStyle returns a style for muted/secondary text.
func (t Theme) MutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Muted)
}

// HighlightStyle returns a style for highlighted text.
func (t Theme) HighlightStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Highlight)
}

// DiffAddStyle returns a style for added diff lines.
func (t Theme) DiffAddStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Success)
}

// DiffRemoveStyle returns a style for removed diff lines.
func (t Theme) DiffRemoveStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Error)
}

// DiffHeaderStyle returns a style for diff headers.
func (t Theme) DiffHeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
}

// ChromaStyle returns a chroma syntax-highlighting style derived from the theme.
// When pywal is active (~/.cache/wal/colors.json exists), the style
// automatically uses the pywal palette via LoadTheme. The mapping is:
//
//	color1 (Error)     → keywords, operators
//	color2 (Success)   → strings, attributes
//	color3 (Warning)   → literals, dates
//	color4 (Accent)    → tags, builtins, headings
//	color5 (Highlight) → numbers
//	color8 (Muted)     → comments
func (t Theme) ChromaStyle() *chroma.Style {
	bg := string(t.Bg)
	fg := string(t.Fg)
	accent := string(t.Accent)
	success := string(t.Success)
	errColor := string(t.Error)
	warning := string(t.Warning)
	muted := string(t.Muted)
	highlight := string(t.Highlight)

	return chroma.MustNewStyle("hydra", chroma.StyleEntries{
		chroma.Background:         fg + " bg:" + bg,
		chroma.Text:               fg,
		chroma.Keyword:            errColor + " bold",
		chroma.KeywordNamespace:   errColor,
		chroma.Name:               fg,
		chroma.NameTag:            accent + " bold",
		chroma.NameAttribute:      success,
		chroma.NameBuiltin:        accent,
		chroma.LiteralString:      success,
		chroma.LiteralStringOther: success,
		chroma.LiteralNumber:      highlight,
		chroma.Literal:            warning,
		chroma.LiteralDate:        warning,
		chroma.Operator:           errColor,
		chroma.Punctuation:        fg,
		chroma.Comment:            muted + " italic",
		chroma.GenericHeading:     accent + " bold",
		chroma.GenericSubheading:  accent,
		chroma.GenericStrong:      "bold",
		chroma.GenericEmph:        "italic",
	})
}
