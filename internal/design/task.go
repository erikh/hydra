package design

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TaskState represents the lifecycle state of a task.
type TaskState string

const (
	// StatePending is a task that has not yet been run.
	StatePending TaskState = "pending"
	// StateReview is a task that has been run and is awaiting review.
	StateReview TaskState = "review"
	// StateMerge is a task that has passed review and is ready to merge.
	StateMerge TaskState = "merge"
	// StateCompleted is a task that has completed the full lifecycle.
	StateCompleted TaskState = "completed"
	// StateAbandoned is a task that has been abandoned.
	StateAbandoned TaskState = "abandoned"
)

// Task represents a single design task.
type Task struct {
	Name     string
	FilePath string
	Group    string
	State    TaskState
}

// Content reads and returns the task's markdown content.
func (t *Task) Content() (string, error) {
	data, err := os.ReadFile(t.FilePath)
	if err != nil {
		return "", fmt.Errorf("reading task %s: %w", t.Name, err)
	}
	return string(data), nil
}

// BranchName returns the normalized git branch name for this task.
func (t *Task) BranchName() string {
	name := t.Name
	if t.Group != "" {
		name = t.Group + "/" + name
	}
	normalized := strings.ToLower(name)
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return "hydra/" + normalized
}

func (d *Dir) discoverTasks(dir string, group string, state TaskState) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	var tasks []Task
	for _, entry := range entries {
		if entry.IsDir() {
			if state == StatePending {
				subTasks, err := d.discoverTasks(
					filepath.Join(dir, entry.Name()),
					entry.Name(),
					state,
				)
				if err != nil {
					return nil, err
				}
				tasks = append(tasks, subTasks...)
			}
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		if entry.Name() == "group.md" {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		tasks = append(tasks, Task{
			Name:     name,
			FilePath: filepath.Join(dir, entry.Name()),
			Group:    group,
			State:    state,
		})
	}

	return tasks, nil
}

// PendingTasks returns all tasks in the tasks/ directory.
func (d *Dir) PendingTasks() ([]Task, error) {
	return d.discoverTasks(filepath.Join(d.Path, "tasks"), "", StatePending)
}

// TasksByState returns all tasks in the given state.
func (d *Dir) TasksByState(state TaskState) ([]Task, error) {
	switch state {
	case StatePending:
		return d.PendingTasks()
	case StateReview, StateMerge, StateCompleted, StateAbandoned:
		return d.discoverTasks(filepath.Join(d.Path, "state", string(state)), "", state)
	default:
		return nil, fmt.Errorf("unknown state: %s", state)
	}
}

// AllTasks returns tasks across all states.
func (d *Dir) AllTasks() ([]Task, error) {
	var all []Task
	for _, state := range []TaskState{StatePending, StateReview, StateMerge, StateCompleted, StateAbandoned} {
		tasks, err := d.TasksByState(state)
		if err != nil {
			return nil, err
		}
		all = append(all, tasks...)
	}
	return all, nil
}

// FindTask looks up a pending task by name or group/name.
func (d *Dir) FindTask(name string) (*Task, error) {
	tasks, err := d.PendingTasks()
	if err != nil {
		return nil, err
	}

	for _, t := range tasks {
		if t.Name == name {
			return &t, nil
		}
		if t.Group != "" && (t.Group+"/"+t.Name) == name {
			return &t, nil
		}
	}

	return nil, fmt.Errorf("task %q not found in pending tasks", name)
}

// FindTaskByState looks up a task by name in the given state.
func (d *Dir) FindTaskByState(name string, state TaskState) (*Task, error) {
	tasks, err := d.TasksByState(state)
	if err != nil {
		return nil, err
	}

	for _, t := range tasks {
		if t.Name == name {
			return &t, nil
		}
		if t.Group != "" && (t.Group+"/"+t.Name) == name {
			return &t, nil
		}
	}

	return nil, fmt.Errorf("task %q not found in %s state", name, state)
}

// FindTaskAny looks up a task by name across all states.
func (d *Dir) FindTaskAny(name string) (*Task, error) {
	tasks, err := d.AllTasks()
	if err != nil {
		return nil, err
	}

	for _, t := range tasks {
		if t.Name == name {
			return &t, nil
		}
		if t.Group != "" && (t.Group+"/"+t.Name) == name {
			return &t, nil
		}
	}

	return nil, fmt.Errorf("task %q not found in any state", name)
}

// MoveTask moves a task file to the given state directory.
func (d *Dir) MoveTask(task *Task, newState TaskState) error {
	var destDir string
	switch newState {
	case StateReview, StateMerge, StateCompleted, StateAbandoned:
		destDir = filepath.Join(d.Path, "state", string(newState))
	default:
		return fmt.Errorf("cannot move task to state: %s", newState)
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	destPath := filepath.Join(destDir, filepath.Base(task.FilePath))
	if err := os.Rename(task.FilePath, destPath); err != nil {
		return fmt.Errorf("moving task file: %w", err)
	}

	task.FilePath = destPath
	task.State = newState
	return nil
}

// DeleteTask removes a task file from disk.
func (d *Dir) DeleteTask(task *Task) error {
	return os.Remove(task.FilePath)
}
