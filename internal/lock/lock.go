// Package lock provides file-based locking with stale PID detection.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockFile is the name of the lock file within the hydra directory.
const LockFile = "hydra.lock"

type lockData struct {
	PID      int    `json:"pid"`
	TaskName string `json:"task_name"`
}

// Lock provides mutual exclusion for hydra task runs using a file-based lock.
type Lock struct {
	path     string
	taskName string
}

// New creates a new Lock for the given hydra directory and task name.
func New(hydraDir, taskName string) *Lock {
	return &Lock{
		path:     filepath.Join(hydraDir, LockFile),
		taskName: taskName,
	}
}

// Acquire attempts to acquire the lock. It returns an error if another live process holds it.
// Stale locks from dead processes are automatically cleaned up.
func (l *Lock) Acquire() error {
	existing, err := l.read()
	if err == nil && existing != nil {
		if processAlive(existing.PID) {
			return fmt.Errorf("another task %q is already running (PID %d)", existing.TaskName, existing.PID)
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

// ReadCurrent reads the current lock and returns the task name and PID if the lock is held by a live process.
func ReadCurrent(hydraDir string) (string, int, error) {
	l := &Lock{path: filepath.Join(hydraDir, LockFile)}
	ld, err := l.read()
	if err != nil {
		return "", 0, err
	}

	if !processAlive(ld.PID) {
		return "", 0, errors.New("no active task (stale lock)")
	}

	return ld.TaskName, ld.PID, nil
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
