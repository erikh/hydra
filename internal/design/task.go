package design

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type TaskState string

const (
	StatePending   TaskState = "pending"
	StateReview    TaskState = "review"
	StateMerge     TaskState = "merge"
	StateCompleted TaskState = "completed"
	StateAbandoned TaskState = "abandoned"
)

type Task struct {
	Name     string
	FilePath string
	Group    string
	State    TaskState
}

func (t *Task) Content() (string, error) {
	data, err := os.ReadFile(t.FilePath)
	if err != nil {
		return "", fmt.Errorf("reading task %s: %w", t.Name, err)
	}
	return string(data), nil
}

func (t *Task) BranchName() string {
	name := t.Name
	if t.Group != "" {
		name = t.Group + "/" + name
	}
	normalized := strings.ToLower(name)
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return "hydra/" + normalized
}

func (d *DesignDir) discoverTasks(dir string, group string, state TaskState) ([]Task, error) {
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

func (d *DesignDir) PendingTasks() ([]Task, error) {
	return d.discoverTasks(filepath.Join(d.Path, "tasks"), "", StatePending)
}

func (d *DesignDir) TasksByState(state TaskState) ([]Task, error) {
	switch state {
	case StatePending:
		return d.PendingTasks()
	case StateReview, StateMerge, StateCompleted, StateAbandoned:
		return d.discoverTasks(filepath.Join(d.Path, "state", string(state)), "", state)
	default:
		return nil, fmt.Errorf("unknown state: %s", state)
	}
}

func (d *DesignDir) AllTasks() ([]Task, error) {
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

func (d *DesignDir) FindTask(name string) (*Task, error) {
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

func (d *DesignDir) MoveTask(task *Task, newState TaskState) error {
	var destDir string
	switch newState {
	case StateReview, StateMerge, StateCompleted, StateAbandoned:
		destDir = filepath.Join(d.Path, "state", string(newState))
	default:
		return fmt.Errorf("cannot move task to state: %s", newState)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
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
