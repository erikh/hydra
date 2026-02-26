package design

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOtherFiles(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "other", "notes.md"), []byte("notes"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "other", "diagram.txt"), []byte("diagram"), 0o600))

	dd, _ := NewDir(dir)
	files, err := dd.OtherFiles()
	if err != nil {
		t.Fatalf("OtherFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}
	if !found["notes.md"] || !found["diagram.txt"] {
		t.Errorf("files = %v", files)
	}
}

func TestOtherFilesEmpty(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))

	dd, _ := NewDir(dir)
	files, err := dd.OtherFiles()
	if err != nil {
		t.Fatalf("OtherFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestOtherFilesNoDir(t *testing.T) {
	dir := t.TempDir()
	dd, _ := NewDir(dir)
	files, err := dd.OtherFiles()
	if err != nil {
		t.Fatalf("OtherFiles: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil, got %v", files)
	}
}

func TestOtherContent(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "other", "notes.md"), []byte("my notes"), 0o600))

	dd, _ := NewDir(dir)
	content, err := dd.OtherContent("notes.md")
	if err != nil {
		t.Fatalf("OtherContent: %v", err)
	}
	if content != "my notes" {
		t.Errorf("content = %q, want %q", content, "my notes")
	}
}

func TestOtherContentNotFound(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))

	dd, _ := NewDir(dir)
	_, err := dd.OtherContent("missing.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want not found", err)
	}
}

func TestRemoveOtherFile(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "other", "notes.md"), []byte("notes"), 0o600))

	dd, _ := NewDir(dir)
	if err := dd.RemoveOtherFile("notes.md"); err != nil {
		t.Fatalf("RemoveOtherFile: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "other", "notes.md")); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}
}

func TestRemoveOtherFileNotFound(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))

	dd, _ := NewDir(dir)
	err := dd.RemoveOtherFile("missing.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want not found", err)
	}
}

func TestAddOtherFile(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))

	editor := writeMockEditor(t, "new file content")
	err := AddOtherFile(dir, "notes.md", editor, nil, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("AddOtherFile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "other", "notes.md")) //nolint:gosec // test
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != "new file content" {
		t.Errorf("content = %q, want %q", string(data), "new file content")
	}
}

func TestAddOtherFileAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "other", "notes.md"), []byte("existing"), 0o600))

	editor := writeMockEditor(t, "new content")
	err := AddOtherFile(dir, "notes.md", editor, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error when file already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want already exists", err)
	}
}

func TestEditOtherFile(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "other", "notes.md"), []byte("original"), 0o600))

	editor := writeMockEditorAppend(t, " edited")
	err := EditOtherFile(dir, "notes.md", editor, nil, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("EditOtherFile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "other", "notes.md")) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original edited" {
		t.Errorf("content = %q, want %q", string(data), "original edited")
	}
}

func TestEditOtherFileNotFound(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))

	editor := writeMockEditor(t, "content")
	err := EditOtherFile(dir, "missing.md", editor, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want not found", err)
	}
}

func TestOtherFileValidation(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "other"), 0o750))
	dd, _ := NewDir(dir)

	// Slash in name.
	_, err := dd.OtherContent("sub/file.md")
	if err == nil {
		t.Error("expected error for name with slash")
	}

	// Double dots.
	err = dd.RemoveOtherFile("../etc/passwd")
	if err == nil {
		t.Error("expected error for name with ..")
	}

	// Empty name.
	_, err = dd.OtherContent("")
	if err == nil {
		t.Error("expected error for empty name")
	}

	// Validate on add/edit too.
	editor := writeMockEditor(t, "content")
	err = AddOtherFile(dir, "sub/file.md", editor, nil, io.Discard, io.Discard)
	if err == nil {
		t.Error("expected error for name with slash in AddOtherFile")
	}

	err = EditOtherFile(dir, "../escape", editor, nil, io.Discard, io.Discard)
	if err == nil {
		t.Error("expected error for name with .. in EditOtherFile")
	}
}
