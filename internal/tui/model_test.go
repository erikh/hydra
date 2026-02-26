package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/erikh/hydra/internal/claude"
)

// newTestModel creates a Model with a buffered session for testing.
// The returned channel can be used to read tool answers.
func newTestModel(autoAccept bool) (Model, chan claude.ToolAnswer) {
	events := make(chan claude.Event, 10)
	answers := make(chan claude.ToolAnswer, 10)

	session := &claude.Session{
		Events:     events,
		ToolAnswer: answers,
	}

	m := New(session, "test-model", autoAccept)
	// Simulate a window size so the model is ready.
	m.width = 80
	m.height = 24
	m.ready = true

	return m, answers
}

func TestModelInitialState(t *testing.T) {
	m, _ := newTestModel(false)

	if m.state != StateStreaming {
		t.Errorf("initial state = %d, want StateStreaming", m.state)
	}
	if m.autoAccept {
		t.Error("autoAccept should be false by default")
	}
}

func TestModelInitialStateAutoAccept(t *testing.T) {
	m, _ := newTestModel(true)

	if !m.autoAccept {
		t.Error("autoAccept should be true when passed")
	}
	if !m.statusbar.AutoAccept {
		t.Error("statusbar.AutoAccept should be true when passed")
	}
}

func TestHandleEventText(t *testing.T) {
	m, _ := newTestModel(false)

	cmds := handleEvent(&m, eventMsg{event: claude.EventText{Text: "hello "}})
	if len(cmds) == 0 {
		t.Error("expected command to wait for next event")
	}
	if !strings.Contains(m.output.String(), "hello ") {
		t.Errorf("output should contain text, got %q", m.output.String())
	}

	handleEvent(&m, eventMsg{event: claude.EventText{Text: "world"}})
	if !strings.Contains(m.output.String(), "hello world") {
		t.Errorf("output should accumulate text, got %q", m.output.String())
	}
}

func TestHandleEventThinking(t *testing.T) {
	m, _ := newTestModel(false)

	handleEvent(&m, eventMsg{event: claude.EventThinking{Text: "thinking..."}})
	if !strings.Contains(m.output.String(), "thinking...") {
		t.Errorf("output should contain thinking text, got %q", m.output.String())
	}
}

func TestHandleEventToolRequestAutoAccept(t *testing.T) {
	m, answers := newTestModel(true)

	evt := claude.EventToolRequest{
		ID:   "tool-1",
		Name: "write_file",
		Meta: claude.ToolMeta{Kind: claude.ToolKindWrite, Path: "f.go"},
	}
	handleEvent(&m, eventMsg{event: evt})

	// Should have auto-approved.
	select {
	case answer := <-answers:
		if !answer.Approved {
			t.Error("auto-accept should approve")
		}
		if answer.ID != "tool-1" {
			t.Errorf("answer ID = %q, want tool-1", answer.ID)
		}
	default:
		t.Error("expected an auto-approve answer on the channel")
	}

	if m.state != StateStreaming {
		t.Errorf("state should remain Streaming during auto-accept, got %d", m.state)
	}
	if !strings.Contains(m.output.String(), "[auto]") {
		t.Errorf("output should contain [auto] marker, got %q", m.output.String())
	}
}

func TestHandleEventToolRequestReadAutoApproved(t *testing.T) {
	m, answers := newTestModel(false)

	// Read-only tools should be auto-approved even when autoAccept is off.
	evt := claude.EventToolRequest{
		ID:   "tool-2",
		Name: "read_file",
		Meta: claude.ToolMeta{Kind: claude.ToolKindRead, Path: "f.go"},
	}
	handleEvent(&m, eventMsg{event: evt})

	select {
	case answer := <-answers:
		if !answer.Approved {
			t.Error("read-only tool should be auto-approved")
		}
	default:
		t.Error("expected auto-approve for read-only tool")
	}
}

func TestHandleEventToolRequestNeedsApproval(t *testing.T) {
	m, _ := newTestModel(false)

	evt := claude.EventToolRequest{
		ID:   "tool-3",
		Name: "bash",
		Meta: claude.ToolMeta{Kind: claude.ToolKindBash, Command: "rm -rf /"},
	}
	handleEvent(&m, eventMsg{event: evt})

	if m.state != StateAwaitingApproval {
		t.Errorf("state should be StateAwaitingApproval, got %d", m.state)
	}
	if m.approval == nil {
		t.Fatal("approval dialog should be set")
	}
	if m.approval.Request.ID != "tool-3" {
		t.Errorf("approval request ID = %q, want tool-3", m.approval.Request.ID)
	}
}

func TestHandleEventToolResult(t *testing.T) {
	m, _ := newTestModel(false)

	handleEvent(&m, eventMsg{event: claude.EventToolResult{
		ID: "tool-1", Content: "file written", IsError: false,
	}})
	if !strings.Contains(m.output.String(), "[ok]") {
		t.Errorf("output should contain [ok] prefix, got %q", m.output.String())
	}
	if !strings.Contains(m.output.String(), "file written") {
		t.Errorf("output should contain result content, got %q", m.output.String())
	}
}

func TestHandleEventToolResultError(t *testing.T) {
	m, _ := newTestModel(false)

	handleEvent(&m, eventMsg{event: claude.EventToolResult{
		ID: "tool-1", Content: "permission denied", IsError: true,
	}})
	if !strings.Contains(m.output.String(), "[err]") {
		t.Errorf("output should contain [err] prefix, got %q", m.output.String())
	}
}

func TestHandleEventDone(t *testing.T) {
	m, _ := newTestModel(false)

	cmds := handleEvent(&m, eventMsg{event: claude.EventDone{StopReason: "end_turn"}})
	if m.state != StateCompleted {
		t.Errorf("state should be StateCompleted, got %d", m.state)
	}
	if m.statusbar.State != "Completed" {
		t.Errorf("statusbar state = %q, want Completed", m.statusbar.State)
	}
	if !strings.Contains(m.output.String(), "end_turn") {
		t.Errorf("output should contain stop reason, got %q", m.output.String())
	}
	// EventDone should not return any commands (no more waiting for events).
	if len(cmds) != 0 {
		t.Errorf("EventDone should return no commands, got %d", len(cmds))
	}
}

func TestHandleEventError(t *testing.T) {
	m, _ := newTestModel(false)

	handleEvent(&m, eventMsg{event: claude.EventError{Err: errors.New("api timeout")}})
	if m.state != StateError {
		t.Errorf("state should be StateError, got %d", m.state)
	}
	if m.err == nil {
		t.Error("model.err should be set")
	}
	if !strings.Contains(m.output.String(), "api timeout") {
		t.Errorf("output should contain error, got %q", m.output.String())
	}
}

func TestUpdateToggleAutoAccept(t *testing.T) {
	m, _ := newTestModel(false)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	updated, _ := m.Update(msg)
	m = updated.(Model) //nolint:forcetypeassert // test

	if !m.autoAccept {
		t.Error("pressing 'a' should enable auto-accept")
	}
	if !m.statusbar.AutoAccept {
		t.Error("statusbar should reflect auto-accept toggle")
	}

	updated, _ = m.Update(msg)
	m = updated.(Model) //nolint:forcetypeassert // test
	if m.autoAccept {
		t.Error("pressing 'a' again should disable auto-accept")
	}
}

func TestUpdateAutoAcceptApprovesCurrentDialog(t *testing.T) {
	m, answers := newTestModel(false)

	// Manually set up an awaiting approval state.
	m.state = StateAwaitingApproval
	m.approval = &ApprovalDialog{
		Request: claude.EventToolRequest{
			ID:   "tool-5",
			Name: "bash",
			Meta: claude.ToolMeta{Kind: claude.ToolKindBash, Command: "echo hi"},
		},
		Theme: m.theme,
		Width: m.width,
	}

	// Press 'a' to enable auto-accept — should also approve the pending dialog.
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	updated, _ := m.Update(msg)
	m = updated.(Model) //nolint:forcetypeassert // test

	if m.state != StateStreaming {
		t.Errorf("state should return to Streaming after auto-accept, got %d", m.state)
	}
	if m.approval != nil {
		t.Error("approval should be nil after auto-accept")
	}

	select {
	case answer := <-answers:
		if !answer.Approved {
			t.Error("toggling auto-accept should approve pending tool")
		}
		if answer.ID != "tool-5" {
			t.Errorf("answer ID = %q, want tool-5", answer.ID)
		}
	default:
		t.Error("expected approve answer on channel")
	}
}

func TestUpdateApproveToolEnter(t *testing.T) {
	m, answers := newTestModel(false)

	m.state = StateAwaitingApproval
	m.approval = &ApprovalDialog{
		Request:  claude.EventToolRequest{ID: "tool-6", Name: "write_file"},
		Selected: 0, // Accept selected
		Theme:    m.theme,
		Width:    m.width,
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ := m.Update(msg)
	m = updated.(Model) //nolint:forcetypeassert // test

	if m.state != StateStreaming {
		t.Errorf("state should return to Streaming, got %d", m.state)
	}

	select {
	case answer := <-answers:
		if !answer.Approved {
			t.Error("enter with Accept selected should approve")
		}
	default:
		t.Error("expected approve answer")
	}
}

func TestUpdateRejectToolEsc(t *testing.T) {
	m, answers := newTestModel(false)

	m.state = StateAwaitingApproval
	m.approval = &ApprovalDialog{
		Request: claude.EventToolRequest{ID: "tool-7", Name: "bash"},
		Theme:   m.theme,
		Width:   m.width,
	}

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	updated, _ := m.Update(msg)
	m = updated.(Model) //nolint:forcetypeassert // test

	if m.state != StateStreaming {
		t.Errorf("state should return to Streaming, got %d", m.state)
	}

	select {
	case answer := <-answers:
		if answer.Approved {
			t.Error("esc should reject")
		}
	default:
		t.Error("expected reject answer")
	}
}

func TestUpdateNavigateButtons(t *testing.T) {
	m, _ := newTestModel(false)

	m.state = StateAwaitingApproval
	m.approval = &ApprovalDialog{
		Request:  claude.EventToolRequest{ID: "tool-8", Name: "bash"},
		Selected: 0,
		Theme:    m.theme,
		Width:    m.width,
	}

	// Navigate right — should select Reject.
	msg := tea.KeyMsg{Type: tea.KeyRight}
	updated, _ := m.Update(msg)
	m = updated.(Model) //nolint:forcetypeassert // test
	if m.approval.Selected != 1 {
		t.Errorf("right arrow should select Reject (1), got %d", m.approval.Selected)
	}

	// Navigate left — should select Accept.
	msg = tea.KeyMsg{Type: tea.KeyLeft}
	updated, _ = m.Update(msg)
	m = updated.(Model) //nolint:forcetypeassert // test
	if m.approval.Selected != 0 {
		t.Errorf("left arrow should select Accept (0), got %d", m.approval.Selected)
	}
}

func TestUpdateEnterWithRejectSelected(t *testing.T) {
	m, answers := newTestModel(false)

	// Select Reject (index 1), then press Enter — should not approve.
	m.state = StateAwaitingApproval
	m.approval = &ApprovalDialog{
		Request:  claude.EventToolRequest{ID: "tool-9", Name: "bash"},
		Selected: 1,
		Theme:    m.theme,
		Width:    m.width,
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ := m.Update(msg)
	m = updated.(Model) //nolint:forcetypeassert // test

	// Enter only approves when Selected == 0, so nothing should happen.
	if m.state != StateAwaitingApproval {
		t.Errorf("enter with Reject selected should not change state, got %d", m.state)
	}
	select {
	case answer := <-answers:
		t.Errorf("should not send answer when Reject is selected with Enter, got %+v", answer)
	default:
		// Expected: no answer sent.
	}
}

func TestViewNotReady(t *testing.T) {
	m, _ := newTestModel(false)
	m.ready = false

	view := m.View()
	if view != stateInitializing {
		t.Errorf("view before ready = %q, want %q", view, stateInitializing)
	}
}

func TestViewReady(t *testing.T) {
	m, _ := newTestModel(false)

	// Initialize viewport.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model) //nolint:forcetypeassert // test

	view := m.View()
	if view == stateInitializing {
		t.Error("view after WindowSizeMsg should not be 'Initializing...'")
	}
	// Should contain status bar content.
	if !strings.Contains(view, "test-model") {
		t.Error("view should contain model name from status bar")
	}
}

func TestViewShowsApprovalDialog(t *testing.T) {
	m, _ := newTestModel(false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model) //nolint:forcetypeassert // test

	m.state = StateAwaitingApproval
	m.approval = &ApprovalDialog{
		Request: claude.EventToolRequest{
			ID: "tool-10", Name: "bash",
			Meta: claude.ToolMeta{Kind: claude.ToolKindBash, Command: "make test"},
		},
		Theme: m.theme,
		Width: m.width,
	}

	view := m.View()
	if !strings.Contains(view, "Accept") {
		t.Error("view should show approval dialog with Accept button")
	}
	if !strings.Contains(view, "make test") {
		t.Error("view should show command in approval dialog")
	}
}

func TestModelErr(t *testing.T) {
	m, _ := newTestModel(false)
	if m.Err() != nil {
		t.Error("initial error should be nil")
	}

	m.err = errors.New("test error")
	if m.Err() == nil || m.Err().Error() != "test error" {
		t.Errorf("Err() = %v, want 'test error'", m.Err())
	}
}

func TestToolSummary(t *testing.T) {
	tests := []struct {
		name string
		evt  claude.EventToolRequest
		want string
	}{
		{
			name: "read file",
			evt: claude.EventToolRequest{
				Name: "read_file",
				Meta: claude.ToolMeta{Kind: claude.ToolKindRead, Path: "src/main.go"},
			},
			want: "src/main.go",
		},
		{
			name: "write file",
			evt: claude.EventToolRequest{
				Name: "write_file",
				Meta: claude.ToolMeta{Kind: claude.ToolKindWrite, Path: "out.txt"},
			},
			want: "out.txt",
		},
		{
			name: "bash short",
			evt: claude.EventToolRequest{
				Name: "bash",
				Meta: claude.ToolMeta{Kind: claude.ToolKindBash, Command: "echo hi"},
			},
			want: "echo hi",
		},
		{
			name: "bash long truncated",
			evt: claude.EventToolRequest{
				Name: "bash",
				Meta: claude.ToolMeta{Kind: claude.ToolKindBash, Command: strings.Repeat("x", 100)},
			},
			want: strings.Repeat("x", 77) + "...",
		},
		{
			name: "unknown kind",
			evt: claude.EventToolRequest{
				Name: "custom_tool",
				Meta: claude.ToolMeta{Kind: claude.ToolKind(99)},
			},
			want: "custom_tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolSummary(tt.evt)
			if got != tt.want {
				t.Errorf("toolSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is longer", 10, "this is..."},
		{"line1\nline2", 20, "line1"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q/%d", tt.input, tt.maxLen), func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
