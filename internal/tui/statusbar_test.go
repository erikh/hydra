package tui

import (
	"strings"
	"testing"
)

func TestStatusBarViewContainsFields(t *testing.T) {
	sb := StatusBar{
		Model:      "claude-sonnet-4-6",
		State:      "Streaming",
		AutoAccept: false,
		Theme:      DefaultTheme(),
		Width:      80,
	}

	view := sb.View()
	for _, want := range []string{"claude-sonnet-4-6", "Streaming", "Auto: OFF"} {
		if !strings.Contains(view, want) {
			t.Errorf("status bar missing %q:\n%s", want, view)
		}
	}
}

func TestStatusBarAutoAcceptOn(t *testing.T) {
	sb := StatusBar{
		Model:      "test-model",
		State:      "Streaming",
		AutoAccept: true,
		Theme:      DefaultTheme(),
		Width:      80,
	}

	view := sb.View()
	if !strings.Contains(view, "Auto: ON") {
		t.Errorf("status bar should show Auto: ON:\n%s", view)
	}
	if strings.Contains(view, "Auto: OFF") {
		t.Errorf("status bar should not show Auto: OFF when enabled:\n%s", view)
	}
}

func TestStatusBarShowsState(t *testing.T) {
	for _, state := range []string{"Streaming", "Awaiting Approval", "Completed", "Error"} {
		sb := StatusBar{
			Model: "m",
			State: state,
			Theme: DefaultTheme(),
			Width: 120,
		}
		view := sb.View()
		if !strings.Contains(view, state) {
			t.Errorf("status bar should contain state %q:\n%s", state, view)
		}
	}
}
