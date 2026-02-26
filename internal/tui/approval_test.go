package tui

import (
	"strings"
	"testing"

	"github.com/erikh/hydra/internal/claude"
)

func TestApprovalDialogWriteTool(t *testing.T) {
	dialog := ApprovalDialog{
		Request: claude.EventToolRequest{
			ID:   "tool-1",
			Name: "write_file",
			Meta: claude.ToolMeta{
				Kind: claude.ToolKindWrite,
				Path: "src/main.go",
				Diff: "--- a/src/main.go\n+++ b/src/main.go\n-old\n+new\n",
			},
		},
		Selected: 0,
		Theme:    DefaultTheme(),
		Width:    80,
	}

	view := dialog.View()
	if !strings.Contains(view, "write_file") {
		t.Error("approval dialog should show tool name")
	}
	if !strings.Contains(view, "src/main.go") {
		t.Error("approval dialog should show file path")
	}
	if !strings.Contains(view, "old") || !strings.Contains(view, "new") {
		t.Error("approval dialog should show diff content")
	}
	if !strings.Contains(view, "Accept") {
		t.Error("approval dialog should show Accept button")
	}
	if !strings.Contains(view, "Reject") {
		t.Error("approval dialog should show Reject button")
	}
}

func TestApprovalDialogBashTool(t *testing.T) {
	dialog := ApprovalDialog{
		Request: claude.EventToolRequest{
			ID:   "tool-2",
			Name: "bash",
			Meta: claude.ToolMeta{
				Kind:    claude.ToolKindBash,
				Command: "go test ./...",
			},
		},
		Selected: 0,
		Theme:    DefaultTheme(),
		Width:    80,
	}

	view := dialog.View()
	if !strings.Contains(view, "bash") {
		t.Error("approval dialog should show tool name")
	}
	if !strings.Contains(view, "go test ./...") {
		t.Error("approval dialog should show command")
	}
}

func TestApprovalDialogEditTool(t *testing.T) {
	dialog := ApprovalDialog{
		Request: claude.EventToolRequest{
			ID:   "tool-3",
			Name: "edit_file",
			Meta: claude.ToolMeta{
				Kind: claude.ToolKindEdit,
				Path: "pkg/foo.go",
				Diff: "--- a/pkg/foo.go\n+++ b/pkg/foo.go\n-bar\n+baz\n",
			},
		},
		Selected: 0,
		Theme:    DefaultTheme(),
		Width:    80,
	}

	view := dialog.View()
	if !strings.Contains(view, "edit_file") {
		t.Error("approval dialog should show tool name")
	}
	if !strings.Contains(view, "pkg/foo.go") {
		t.Error("approval dialog should show file path")
	}
}

func TestApprovalDialogReadTool(t *testing.T) {
	dialog := ApprovalDialog{
		Request: claude.EventToolRequest{
			ID:   "tool-4",
			Name: "read_file",
			Meta: claude.ToolMeta{
				Kind: claude.ToolKindRead,
				Path: "README.md",
			},
		},
		Selected: 0,
		Theme:    DefaultTheme(),
		Width:    80,
	}

	view := dialog.View()
	if !strings.Contains(view, "read_file") {
		t.Error("approval dialog should show tool name")
	}
	if !strings.Contains(view, "README.md") {
		t.Error("approval dialog should show path")
	}
}

func TestApprovalDialogNoDiffForWriteWithoutDiff(t *testing.T) {
	dialog := ApprovalDialog{
		Request: claude.EventToolRequest{
			ID:   "tool-5",
			Name: "write_file",
			Meta: claude.ToolMeta{
				Kind: claude.ToolKindWrite,
				Path: "new.go",
				Diff: "",
			},
		},
		Selected: 0,
		Theme:    DefaultTheme(),
		Width:    80,
	}

	view := dialog.View()
	if !strings.Contains(view, "new.go") {
		t.Error("approval dialog should show file path")
	}
	// Should not render "Diff:" header when diff is empty.
	if strings.Contains(view, "Diff:") {
		t.Error("approval dialog should not show Diff header when diff is empty")
	}
}
