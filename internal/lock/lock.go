// Package lock provides file-based locking with stale PID detection.
package lock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type lockData struct {
	PID      int    `json:"pid"`
	TaskName string `json:"task_name"`
}

// RunningTask describes a currently-running hydra task.
type RunningTask struct {
	TaskName string
	PID      int
}

// Lock provides mutual exclusion for hydra task runs using a file-based lock.
type Lock struct {
	path     string
	taskName string
}

// lockFileName returns the per-task lock file name.
// Slashes in grouped task names (e.g. "backend/add-api") are replaced with "--".
func lockFileName(taskName string) string {
	safe := strings.ReplaceAll(taskName, "/", "--")
	return "hydra-" + safe + ".lock"
}

// New creates a new Lock for the given hydra directory and task name.
func New(hydraDir, taskName string) *Lock {
	return &Lock{
		path:     filepath.Join(hydraDir, lockFileName(taskName)),
		taskName: taskName,
	}
}

// Acquire attempts to acquire the lock. It returns an error if another live process holds it.
// Stale locks from dead processes are automatically cleaned up.
func (l *Lock) Acquire() error {
	existing, err := l.read()
	if err == nil && existing != nil {
		if processAlive(existing.PID) {
			return fmt.Errorf("task %q is already running (PID %d)", existing.TaskName, existing.PID)
		}
		// Stale lock, remove it.
		_ = os.Remove(l.path)
	}

	data, err := json.Marshal(&lockData{
		PID:      os.Getpid(),
		TaskName: l.taskName,
	})
	if err != nil {
		return fmt.Errorf("marshaling lock data: %w", err)
	}

	if err := os.WriteFile(l.path, data, 0o600); err != nil {
		return fmt.Errorf("writing lock file: %w", err)
	}

	return nil
}

// Release removes the lock file.
func (l *Lock) Release() error {
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing lock file: %w", err)
	}
	return nil
}

// IsHeld returns true if the lock file exists and is held by a live process.
func (l *Lock) IsHeld() bool {
	existing, err := l.read()
	if err != nil || existing == nil {
		return false
	}
	return processAlive(existing.PID)
}

func (l *Lock) read() (*lockData, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, err
	}

	var ld lockData
	if err := json.Unmarshal(data, &ld); err != nil {
		return nil, err
	}

	return &ld, nil
}

// ReadAll scans the hydra directory for per-task lock files and returns
// all tasks that are currently held by live processes.
func ReadAll(hydraDir string) ([]RunningTask, error) {
	pattern := filepath.Join(hydraDir, "hydra-*.lock")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing lock files: %w", err)
	}

	var running []RunningTask
	for _, path := range matches {
		data, err := os.ReadFile(path) //nolint:gosec // lock files in hydra dir
		if err != nil {
			continue
		}

		var ld lockData
		if err := json.Unmarshal(data, &ld); err != nil {
			continue
		}

		if processAlive(ld.PID) {
			running = append(running, RunningTask{TaskName: ld.TaskName, PID: ld.PID})
		}
	}

	return running, nil
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
