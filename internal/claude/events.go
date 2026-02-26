// Package claude provides direct Anthropic API integration for hydra.
package claude

import "encoding/json"

// Event is the interface for all session events sent to the TUI.
type Event interface {
	eventMarker()
}

// EventText carries a streaming text delta.
type EventText struct {
	Text string
}

func (EventText) eventMarker() {}

// EventThinking carries an extended-thinking delta.
type EventThinking struct {
	Text string
}

func (EventThinking) eventMarker() {}

// ToolKind classifies a tool for display and approval routing.
type ToolKind int

// ToolKind values for each supported tool.
const (
	ToolKindRead ToolKind = iota
	ToolKindWrite
	ToolKindEdit
	ToolKindBash
	ToolKindList
	ToolKindSearch
)

// ToolMeta holds pre-computed display information for a tool request.
type ToolMeta struct {
	Kind    ToolKind
	Path    string // file path for read/write/edit/list/search
	Diff    string // unified diff for write/edit (before execution)
	Command string // shell command for bash
	Content string // file content for write
}

// EventToolRequest signals that the model wants to invoke a tool.
type EventToolRequest struct {
	ID    string
	Name  string
	Input json.RawMessage
	Meta  ToolMeta
}

func (EventToolRequest) eventMarker() {}

// EventToolResult carries the outcome of a tool execution.
type EventToolResult struct {
	ID      string
	Content string
	IsError bool
}

func (EventToolResult) eventMarker() {}

// EventDone signals the conversation has ended.
type EventDone struct {
	StopReason string
}

func (EventDone) eventMarker() {}

// EventError signals a fatal error.
type EventError struct {
	Err error
}

func (EventError) eventMarker() {}

// ToolAnswer is the TUI's response to a tool request.
type ToolAnswer struct {
	ID       string
	Approved bool
}
