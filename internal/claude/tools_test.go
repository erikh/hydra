package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePath(t *testing.T) {
	repoDir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"relative path", "src/main.go", false},
		{"dot path", ".", false},
		{"traversal", "../etc/passwd", true},
		{"abs within repo", filepath.Join(repoDir, "file.go"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePath(repoDir, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestExecReadFile(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]string{"path": "test.txt"})
	result, err := ExecuteTool(repoDir, "read_file", input)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
}

func TestExecWriteFile(t *testing.T) {
	repoDir := t.TempDir()

	input, _ := json.Marshal(map[string]string{"path": "new.txt", "content": "new content"})
	_, err := ExecuteTool(repoDir, "write_file", input)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(repoDir, "new.txt")) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new content" {
		t.Errorf("file content = %q, want %q", string(data), "new content")
	}
}

func TestExecWriteFileCreatesDirectories(t *testing.T) {
	repoDir := t.TempDir()

	input, _ := json.Marshal(map[string]string{"path": "sub/dir/file.txt", "content": "nested"})
	_, err := ExecuteTool(repoDir, "write_file", input)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(repoDir, "sub", "dir", "file.txt")) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "nested" {
		t.Errorf("file content = %q, want %q", string(data), "nested")
	}
}

func TestExecEditFile(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "edit.txt"), []byte("foo bar baz"), 0o600); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]string{"path": "edit.txt", "old_text": "bar", "new_text": "qux"})
	_, err := ExecuteTool(repoDir, "edit_file", input)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(repoDir, "edit.txt")) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "foo qux baz" {
		t.Errorf("file content = %q, want %q", string(data), "foo qux baz")
	}
}

func TestExecEditFileNotFound(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "edit.txt"), []byte("foo bar baz"), 0o600); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]string{"path": "edit.txt", "old_text": "missing", "new_text": "qux"})
	_, err := ExecuteTool(repoDir, "edit_file", input)
	if err == nil {
		t.Fatal("expected error when old_text not found")
	}
}

func TestExecBash(t *testing.T) {
	repoDir := t.TempDir()

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := ExecuteTool(repoDir, "bash", input)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if strings.TrimSpace(result) != "hello" {
		t.Errorf("result = %q, want %q", strings.TrimSpace(result), "hello")
	}
}

func TestExecListFiles(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "a.go"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "b.txt"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]string{"path": "."})
	result, err := ExecuteTool(repoDir, "list_files", input)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !strings.Contains(result, "a.go") {
		t.Error("result missing a.go")
	}
	if !strings.Contains(result, "sub/") {
		t.Error("result missing sub/")
	}
}

func TestExecListFilesWithPattern(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "a.go"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "b.txt"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]string{"path": ".", "pattern": "*.go"})
	result, err := ExecuteTool(repoDir, "list_files", input)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !strings.Contains(result, "a.go") {
		t.Error("result missing a.go")
	}
	if strings.Contains(result, "b.txt") {
		t.Error("result should not contain b.txt")
	}
}

func TestExecSearchFiles(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]string{"pattern": "func main"})
	result, err := ExecuteTool(repoDir, "search_files", input)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !strings.Contains(result, "func main") {
		t.Errorf("result = %q, want match for 'func main'", result)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	repoDir := t.TempDir()

	input, _ := json.Marshal(map[string]string{"path": "../../etc/passwd"})
	_, err := ExecuteTool(repoDir, "read_file", input)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %q, want escapes message", err)
	}
}

func TestPrepareMeta(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "existing.txt"), []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("write_file with existing file", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{"path": "existing.txt", "content": "new content"})
		meta := PrepareMeta(repoDir, "write_file", input)
		if meta.Kind != ToolKindWrite {
			t.Errorf("Kind = %d, want ToolKindWrite", meta.Kind)
		}
		if meta.Diff == "" {
			t.Error("expected non-empty diff")
		}
	})

	t.Run("bash command", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{"command": "ls -la"})
		meta := PrepareMeta(repoDir, "bash", input)
		if meta.Kind != ToolKindBash {
			t.Errorf("Kind = %d, want ToolKindBash", meta.Kind)
		}
		if meta.Command != "ls -la" {
			t.Errorf("Command = %q, want %q", meta.Command, "ls -la")
		}
	})
}
