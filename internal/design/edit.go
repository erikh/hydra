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

// EditNewTask opens an editor to create a new task file in the design directory.
// It rejects task names containing "/" (no group support yet), checks for duplicates,
// and only saves the file if the editor exits successfully with non-empty content.
func EditNewTask(designDir, taskName, editor string, stdin io.Reader, stdout, stderr io.Writer) error {
	if strings.Contains(taskName, "/") {
		return errors.New("task name must not contain '/' (grouped tasks not supported for edit)")
	}

	tasksDir := filepath.Join(designDir, "tasks")
	destPath := filepath.Join(tasksDir, taskName+".md")

	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("task %q already exists", taskName)
	}

	tmpFile, err := os.CreateTemp("", "hydra-task-*.md")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	cmd := exec.CommandContext(context.Background(), editor, tmpPath) //nolint:gosec // editor is user-provided via $VISUAL/$EDITOR
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
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
