package lock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const LockFile = "hydra.lock"

type lockData struct {
	PID      int    `json:"pid"`
	TaskName string `json:"task_name"`
}

type Lock struct {
	path     string
	taskName string
}

func New(hydraDir, taskName string) *Lock {
	return &Lock{
		path:     filepath.Join(hydraDir, LockFile),
		taskName: taskName,
	}
}

func (l *Lock) Acquire() error {
	existing, err := l.read()
	if err == nil && existing != nil {
		if processAlive(existing.PID) {
			return fmt.Errorf("another task %q is already running (PID %d)", existing.TaskName, existing.PID)
		}
		// Stale lock, remove it
		os.Remove(l.path)
	}

	data, err := json.Marshal(&lockData{
		PID:      os.Getpid(),
		TaskName: l.taskName,
	})
	if err != nil {
		return fmt.Errorf("marshaling lock data: %w", err)
	}

	if err := os.WriteFile(l.path, data, 0o644); err != nil {
		return fmt.Errorf("writing lock file: %w", err)
	}

	return nil
}

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

func ReadCurrent(hydraDir string) (string, int, error) {
	l := &Lock{path: filepath.Join(hydraDir, LockFile)}
	ld, err := l.read()
	if err != nil {
		return "", 0, err
	}

	if !processAlive(ld.PID) {
		return "", 0, fmt.Errorf("no active task (stale lock)")
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
