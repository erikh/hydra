package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/erikh/hydra/internal/claude"
)

// State represents the TUI state.
type State int

// State constants for the TUI model.
const (
	StateStreaming        State = iota // receiving streaming output
	StateAwaitingApproval              // waiting for user tool approval
	StateCompleted                     // session finished successfully
	StateError                         // session ended with an error
)

const (
	stateStreaming    = "Streaming"
	stateInitializing = "Initializing..."
)

// Model is the root Bubbletea model for the Claude session TUI.
type Model struct {
	session    *claude.Session
	theme      Theme
	keymap     KeyMap
	viewport   viewport.Model
	statusbar  StatusBar
	approval   *ApprovalDialog
	state      State
	autoAccept bool
	output     strings.Builder
	err        error
	width      int
	height     int
	ready      bool
}

// eventMsg wraps a claude.Event for the Bubbletea message system.
type eventMsg struct {
	event claude.Event
}

// New creates a new TUI model.
func New(session *claude.Session, model string, autoAccept bool) Model {
	theme := LoadTheme()

	return Model{
		session:    session,
		theme:      theme,
		keymap:     DefaultKeyMap(),
		autoAccept: autoAccept,
		statusbar: StatusBar{
			Model:      model,
			State:      stateStreaming,
			AutoAccept: autoAccept,
			Theme:      theme,
		},
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return m.waitForEvent()
}

// waitForEvent returns a command that waits for the next event from the session.
func (m Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.session.Events
		if !ok {
			return eventMsg{event: claude.EventDone{StopReason: "channel_closed"}}
		}
		return eventMsg{event: event}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.statusbar.Width = msg.Width

		headerHeight := 0
		statusHeight := 1
		vpHeight := m.height - headerHeight - statusHeight - 2

		if !m.ready {
			m.viewport = viewport.New(m.width, vpHeight)
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = vpHeight
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keymap.Quit):
			m.session.Cancel()
			return m, tea.Quit

		case key.Matches(msg, m.keymap.AutoAccept):
			m.autoAccept = !m.autoAccept
			m.statusbar.AutoAccept = m.autoAccept
			// If we just enabled auto-accept and we're awaiting approval, approve it.
			if m.autoAccept && m.state == StateAwaitingApproval && m.approval != nil {
				m.session.ToolAnswer <- claude.ToolAnswer{
					ID:       m.approval.Request.ID,
					Approved: true,
				}
				m.state = StateStreaming
				m.statusbar.State = stateStreaming
				m.approval = nil
			}

		case key.Matches(msg, m.keymap.Approve):
			if m.state == StateAwaitingApproval && m.approval != nil && m.approval.Selected == 0 {
				m.session.ToolAnswer <- claude.ToolAnswer{
					ID:       m.approval.Request.ID,
					Approved: true,
				}
				m.state = StateStreaming
				m.statusbar.State = stateStreaming
				m.approval = nil
			} else if m.state == StateCompleted || m.state == StateError {
				return m, tea.Quit
			}

		case key.Matches(msg, m.keymap.Reject):
			if m.state == StateAwaitingApproval && m.approval != nil {
				m.session.ToolAnswer <- claude.ToolAnswer{
					ID:       m.approval.Request.ID,
					Approved: false,
				}
				m.state = StateStreaming
				m.statusbar.State = stateStreaming
				m.approval = nil
			}

		case key.Matches(msg, m.keymap.NavLeft):
			if m.state == StateAwaitingApproval && m.approval != nil {
				m.approval.Selected = 0
			}

		case key.Matches(msg, m.keymap.NavRight):
			if m.state == StateAwaitingApproval && m.approval != nil {
				m.approval.Selected = 1
			}
		}

	case eventMsg:
		cmds = append(cmds, handleEvent(&m, msg)...)
	}

	// Update viewport for scrolling.
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

// handleEvent processes Claude session events and returns any resulting commands.
func handleEvent(m *Model, msg eventMsg) []tea.Cmd {
	var cmds []tea.Cmd

	switch evt := msg.event.(type) {
	case claude.EventText:
		m.output.WriteString(evt.Text)
		m.viewport.SetContent(m.output.String())
		m.viewport.GotoBottom()
		cmds = append(cmds, m.waitForEvent())

	case claude.EventThinking:
		m.output.WriteString(m.theme.MutedStyle().Render(evt.Text))
		m.viewport.SetContent(m.output.String())
		m.viewport.GotoBottom()
		cmds = append(cmds, m.waitForEvent())

	case claude.EventToolRequest:
		if m.autoAccept || !claude.NeedsApproval(evt.Name) {
			// Auto-approve.
			m.session.ToolAnswer <- claude.ToolAnswer{
				ID:       evt.ID,
				Approved: true,
			}
			m.output.WriteString(m.theme.MutedStyle().Render(
				fmt.Sprintf("\n[auto] %s: %s\n", evt.Name, toolSummary(evt))))
			m.viewport.SetContent(m.output.String())
			m.viewport.GotoBottom()
			cmds = append(cmds, m.waitForEvent())
		} else {
			m.state = StateAwaitingApproval
			m.statusbar.State = "Awaiting Approval"
			m.approval = &ApprovalDialog{
				Request:  evt,
				Selected: 0,
				Theme:    m.theme,
				Width:    m.width,
			}
			cmds = append(cmds, m.waitForEvent())
		}

	case claude.EventToolResult:
		prefix := m.theme.SuccessStyle().Render("[ok]")
		if evt.IsError {
			prefix = m.theme.ErrorStyle().Render("[err]")
		}
		fmt.Fprintf(&m.output, "\n%s %s\n", prefix, truncate(evt.Content, 200))
		m.viewport.SetContent(m.output.String())
		m.viewport.GotoBottom()
		cmds = append(cmds, m.waitForEvent())

	case claude.EventDone:
		m.state = StateCompleted
		m.statusbar.State = "Completed"
		m.output.WriteString(m.theme.SuccessStyle().Render(
			fmt.Sprintf("\n\nSession complete (%s). Press Enter to exit.\n", evt.StopReason)))
		m.viewport.SetContent(m.output.String())
		m.viewport.GotoBottom()

	case claude.EventError:
		m.state = StateError
		m.statusbar.State = "Error"
		m.err = evt.Err
		m.output.WriteString(m.theme.ErrorStyle().Render(
			fmt.Sprintf("\n\nError: %v\nPress Enter to exit.\n", evt.Err)))
		m.viewport.SetContent(m.output.String())
		m.viewport.GotoBottom()
	}

	return cmds
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return stateInitializing
	}

	var sections []string

	// Main viewport.
	sections = append(sections, m.viewport.View())

	// Approval dialog (rendered above status bar when active).
	if m.state == StateAwaitingApproval && m.approval != nil {
		sections = append(sections, m.approval.View())
	}

	// Status bar.
	sections = append(sections, m.statusbar.View())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// Err returns any error that occurred during the session.
func (m Model) Err() error {
	return m.err
}

func toolSummary(evt claude.EventToolRequest) string {
	switch evt.Meta.Kind {
	case claude.ToolKindRead, claude.ToolKindList, claude.ToolKindSearch:
		return evt.Meta.Path
	case claude.ToolKindWrite, claude.ToolKindEdit:
		return evt.Meta.Path
	case claude.ToolKindBash:
		return truncate(evt.Meta.Command, 80)
	default:
		return evt.Name
	}
}

func truncate(s string, maxLen int) string {
	// Only use the first line.
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
