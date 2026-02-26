package claude

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// Tool name constants.
const (
	toolReadFile    = "read_file"
	toolWriteFile   = "write_file"
	toolEditFile    = "edit_file"
	toolBash        = "bash"
	toolListFiles   = "list_files"
	toolSearchFiles = "search_files"
)

// ToolDefinitions returns the six tool schemas for the Anthropic API.
func ToolDefinitions() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		{OfTool: &anthropic.ToolParam{
			Name:        toolReadFile,
			Description: param.NewOpt("Read the contents of a file at the given path."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The file path to read, relative to the repository root.",
					},
				},
				Required: []string{"path"},
			},
		}},
		{OfTool: &anthropic.ToolParam{
			Name:        toolWriteFile,
			Description: param.NewOpt("Write content to a file, creating it if it doesn't exist or overwriting if it does."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The file path to write to, relative to the repository root.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The full content to write to the file.",
					},
				},
				Required: []string{"path", "content"},
			},
		}},
		{OfTool: &anthropic.ToolParam{
			Name:        toolEditFile,
			Description: param.NewOpt("Replace a specific text occurrence in a file with new text."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The file path to edit, relative to the repository root.",
					},
					"old_text": map[string]any{
						"type":        "string",
						"description": "The exact text to find and replace. Must match exactly.",
					},
					"new_text": map[string]any{
						"type":        "string",
						"description": "The replacement text.",
					},
				},
				Required: []string{"path", "old_text", "new_text"},
			},
		}},
		{OfTool: &anthropic.ToolParam{
			Name:        toolBash,
			Description: param.NewOpt("Execute a bash command and return the output."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The bash command to execute.",
					},
				},
				Required: []string{"command"},
			},
		}},
		{OfTool: &anthropic.ToolParam{
			Name:        toolListFiles,
			Description: param.NewOpt("List files and directories at the given path."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The directory path to list, relative to the repository root.",
					},
					"pattern": map[string]any{
						"type":        "string",
						"description": "Optional glob pattern to filter results.",
					},
				},
				Required: []string{"path"},
			},
		}},
		{OfTool: &anthropic.ToolParam{
			Name:        toolSearchFiles,
			Description: param.NewOpt("Search for a regex pattern in files."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "The regex pattern to search for.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Optional directory to search in, relative to the repository root.",
					},
					"glob": map[string]any{
						"type":        "string",
						"description": "Optional glob pattern to filter which files to search.",
					},
				},
				Required: []string{"pattern"},
			},
		}},
	}
}

// NeedsApproval returns true if the tool requires user approval before execution.
func NeedsApproval(name string) bool {
	switch name {
	case toolWriteFile, toolEditFile, toolBash:
		return true
	default:
		return false
	}
}

// ToolKindFor returns the ToolKind for a given tool name.
func ToolKindFor(name string) ToolKind {
	switch name {
	case toolReadFile:
		return ToolKindRead
	case toolWriteFile:
		return ToolKindWrite
	case toolEditFile:
		return ToolKindEdit
	case toolBash:
		return ToolKindBash
	case toolListFiles:
		return ToolKindList
	case toolSearchFiles:
		return ToolKindSearch
	default:
		return ToolKindRead
	}
}
