package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultThemeFieldsNonEmpty(t *testing.T) {
	theme := DefaultTheme()
	fields := map[string]string{
		"Bg":        string(theme.Bg),
		"Fg":        string(theme.Fg),
		"Accent":    string(theme.Accent),
		"Success":   string(theme.Success),
		"Error":     string(theme.Error),
		"Warning":   string(theme.Warning),
		"Muted":     string(theme.Muted),
		"Highlight": string(theme.Highlight),
	}
	for name, val := range fields {
		if val == "" {
			t.Errorf("DefaultTheme().%s is empty", name)
		}
	}
}

func TestLoadThemeFallsBackToDefault(t *testing.T) {
	// Point HOME to a temp dir with no pywal files.
	home := t.TempDir()
	t.Setenv("HOME", home)

	theme := LoadTheme()
	def := DefaultTheme()
	if theme.Bg != def.Bg {
		t.Errorf("Bg = %q, want default %q", theme.Bg, def.Bg)
	}
	if theme.Fg != def.Fg {
		t.Errorf("Fg = %q, want default %q", theme.Fg, def.Fg)
	}
	if theme.Accent != def.Accent {
		t.Errorf("Accent = %q, want default %q", theme.Accent, def.Accent)
	}
}

func TestLoadThemeFromPywal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	walDir := filepath.Join(home, ".cache", "wal")
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		t.Fatal(err)
	}

	colors := `{
		"special": {
			"background": "#111111",
			"foreground": "#eeeeee"
		},
		"colors": {
			"color1": "#ff0000",
			"color2": "#00ff00",
			"color3": "#ffff00",
			"color4": "#0000ff",
			"color5": "#ff00ff",
			"color8": "#888888"
		}
	}`
	if err := os.WriteFile(filepath.Join(walDir, "colors.json"), []byte(colors), 0o600); err != nil {
		t.Fatal(err)
	}

	theme := LoadTheme()

	if string(theme.Bg) != "#111111" {
		t.Errorf("Bg = %q, want #111111", theme.Bg)
	}
	if string(theme.Fg) != "#eeeeee" {
		t.Errorf("Fg = %q, want #eeeeee", theme.Fg)
	}
	if string(theme.Accent) != "#0000ff" {
		t.Errorf("Accent = %q, want #0000ff (color4)", theme.Accent)
	}
	if string(theme.Success) != "#00ff00" {
		t.Errorf("Success = %q, want #00ff00 (color2)", theme.Success)
	}
	if string(theme.Error) != "#ff0000" {
		t.Errorf("Error = %q, want #ff0000 (color1)", theme.Error)
	}
	if string(theme.Warning) != "#ffff00" {
		t.Errorf("Warning = %q, want #ffff00 (color3)", theme.Warning)
	}
	if string(theme.Muted) != "#888888" {
		t.Errorf("Muted = %q, want #888888 (color8)", theme.Muted)
	}
	if string(theme.Highlight) != "#ff00ff" {
		t.Errorf("Highlight = %q, want #ff00ff (color5)", theme.Highlight)
	}
}

func TestLoadThemePywalMalformedJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	walDir := filepath.Join(home, ".cache", "wal")
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(walDir, "colors.json"), []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}

	theme := LoadTheme()
	def := DefaultTheme()
	if theme.Bg != def.Bg {
		t.Errorf("malformed pywal should fall back to default, got Bg=%q", theme.Bg)
	}
}

func TestLoadThemePywalPartialColors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	walDir := filepath.Join(home, ".cache", "wal")
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Only background set, colors map empty — should use defaults for missing colors.
	colors := `{
		"special": {"background": "#222222", "foreground": ""},
		"colors": {}
	}`
	if err := os.WriteFile(filepath.Join(walDir, "colors.json"), []byte(colors), 0o600); err != nil {
		t.Fatal(err)
	}

	theme := LoadTheme()
	def := DefaultTheme()

	if string(theme.Bg) != "#222222" {
		t.Errorf("Bg = %q, want #222222", theme.Bg)
	}
	// Empty foreground should fall back to default.
	if theme.Fg != def.Fg {
		t.Errorf("Fg = %q, want default %q (empty foreground)", theme.Fg, def.Fg)
	}
	// Missing color keys should fall back to defaults.
	if theme.Accent != def.Accent {
		t.Errorf("Accent = %q, want default %q (missing color4)", theme.Accent, def.Accent)
	}
}

func TestThemeStyles(t *testing.T) {
	theme := DefaultTheme()

	// Verify styles render without panicking and produce non-empty output.
	styles := map[string]string{
		"TextStyle":       theme.TextStyle().Render("test"),
		"AccentStyle":     theme.AccentStyle().Render("test"),
		"ErrorStyle":      theme.ErrorStyle().Render("test"),
		"SuccessStyle":    theme.SuccessStyle().Render("test"),
		"WarningStyle":    theme.WarningStyle().Render("test"),
		"MutedStyle":      theme.MutedStyle().Render("test"),
		"HighlightStyle":  theme.HighlightStyle().Render("test"),
		"DiffAddStyle":    theme.DiffAddStyle().Render("test"),
		"DiffRemoveStyle": theme.DiffRemoveStyle().Render("test"),
		"DiffHeaderStyle": theme.DiffHeaderStyle().Render("test"),
	}
	for name, rendered := range styles {
		if rendered == "" {
			t.Errorf("%s rendered empty", name)
		}
	}
}
