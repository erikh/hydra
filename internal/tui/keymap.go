package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keybindings for the TUI.
type KeyMap struct {
	Quit       key.Binding
	AutoAccept key.Binding
	Approve    key.Binding
	Reject     key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	NavLeft    key.Binding
	NavRight   key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		AutoAccept: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "toggle auto-accept"),
		),
		Approve: key.NewBinding(
			key.WithKeys("enter", "y"),
			key.WithHelp("enter/y", "approve tool"),
		),
		Reject: key.NewBinding(
			key.WithKeys("esc", "n"),
			key.WithHelp("esc/n", "reject tool"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("up", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("down", "scroll down"),
		),
		NavLeft: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("left", "select accept"),
		),
		NavRight: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("right", "select reject"),
		),
	}
}
