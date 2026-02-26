package design

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runEditor opens the given file in the specified editor, attaching stdin/stdout/stderr.
func runEditor(editor, filePath string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(context.Background(), editor, filePath)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}
	return nil
}

// RunEditorOnFile opens the given file in the editor. This is the public wrapper
// for the internal runEditor helper, for use by other packages.
func RunEditorOnFile(editor, filePath string, stdin io.Reader, stdout, stderr io.Writer) error {
	return runEditor(editor, filePath, stdin, stdout, stderr)
}

// EditTask opens an editor to create or edit a task file in the design directory.
// If the task already exists in tasks/, it opens the existing file in-place.
// Otherwise, it creates a new task via a temp file (only saving if non-empty).
// Task names must not contain '/'.
func EditTask(designDir, taskName, editor string, stdin io.Reader, stdout, stderr io.Writer) error {
	if strings.Contains(taskName, "/") {
		return errors.New("task name must not contain '/' (grouped tasks not supported for edit)")
	}

	tasksDir := filepath.Join(designDir, "tasks")
	destPath := filepath.Join(tasksDir, taskName+".md")

	// If the task already exists, open it in-place.
	if _, err := os.Stat(destPath); err == nil {
		return runEditor(editor, destPath, stdin, stdout, stderr)
	}

	return createNewTask(designDir, taskName, editor, stdin, stdout, stderr)
}

// createNewTask creates a new task file via a temp file, only saving if non-empty.
func createNewTask(designDir, taskName, editor string, stdin io.Reader, stdout, stderr io.Writer) error {
	tasksDir := filepath.Join(designDir, "tasks")
	destPath := filepath.Join(tasksDir, taskName+".md")

	tmpFile, err := os.CreateTemp("", "hydra-task-*.md")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := runEditor(editor, tmpPath, stdin, stdout, stderr); err != nil {
		return err
	}

	content, err := os.ReadFile(tmpPath) //nolint:gosec // path is from our own temp file
	if err != nil {
		return fmt.Errorf("reading temp file: %w", err)
	}

	if len(strings.TrimSpace(string(content))) == 0 {
		return errors.New("empty task file, aborting")
	}

	if err := os.MkdirAll(tasksDir, 0o750); err != nil {
		return fmt.Errorf("creating tasks directory: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		// Rename can fail across filesystems; fall back to copy.
		if err := os.WriteFile(destPath, content, 0o600); err != nil {
			return fmt.Errorf("writing task file: %w", err)
		}
	}

	return nil
}

// EditNewTask is deprecated; use EditTask instead.
func EditNewTask(designDir, taskName, editor string, stdin io.Reader, stdout, stderr io.Writer) error {
	return EditTask(designDir, taskName, editor, stdin, stdout, stderr)
}
