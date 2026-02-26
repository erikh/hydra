// Package design handles reading and assembling design directory documents.
package design

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir represents a design directory containing rules, lint, functional specs, and tasks.
type Dir struct {
	Path string
}

// NewDir opens and validates a design directory at the given path.
func NewDir(path string) (*Dir, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving design dir: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("accessing design dir: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}

	return &Dir{Path: abs}, nil
}

func (d *Dir) readFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(d.Path, name)) //nolint:gosec // paths are constructed from trusted design dir
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", name, err)
	}
	return string(data), nil
}

// Rules returns the content of rules.md, or empty string if it doesn't exist.
func (d *Dir) Rules() (string, error) {
	return d.readFile("rules.md")
}

// Lint returns the content of lint.md, or empty string if it doesn't exist.
func (d *Dir) Lint() (string, error) {
	return d.readFile("lint.md")
}

// Functional returns the content of functional.md, or empty string if it doesn't exist.
func (d *Dir) Functional() (string, error) {
	return d.readFile("functional.md")
}

// DefaultHydraYml is the placeholder content for a new hydra.yml.
const DefaultHydraYml = `# Commands that Claude runs before committing.
#
# IMPORTANT: These commands may run concurrently across multiple hydra tasks,
# each in its own work directory (cloned repo). Make sure your test and lint
# commands are safe to run in parallel without trampling each other. Avoid
# commands that write to shared global state, fixed file paths outside the
# work directory, or shared network ports. Each invocation should be fully
# isolated to its own working tree.
commands:
  # before: "make deps"
  # clean: "make clean"
  # dev: "npm run dev"
  # lint: "golangci-lint run ./..."
  # test: "go test ./... -count=1"
`

// EnsureHydraYml creates hydra.yml with placeholder content if it does not exist.
func EnsureHydraYml(path string) error {
	p := filepath.Join(path, "hydra.yml")
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	return os.WriteFile(p, []byte(DefaultHydraYml), 0o600)
}

// Scaffold creates the full design directory skeleton tree at the given path.
// If the directory already has content (e.g. rules.md exists), it skips scaffolding
// but still ensures hydra.yml exists.
func Scaffold(path string) error {
	// If rules.md already exists, assume the directory is already scaffolded.
	// Still ensure hydra.yml exists.
	if _, err := os.Stat(filepath.Join(path, "rules.md")); err == nil {
		return EnsureHydraYml(path)
	}

	dirs := []string{
		"tasks",
		"other",
		filepath.Join("state", "review"),
		filepath.Join("state", "merge"),
		filepath.Join("state", "completed"),
		filepath.Join("state", "abandoned"),
		filepath.Join("milestone", "history"),
		filepath.Join("milestone", "delivered"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(path, d), 0o750); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	placeholders := map[string]string{
		"rules.md":                            "",
		"lint.md":                             "",
		"functional.md":                       "",
		"hydra.yml":                           DefaultHydraYml,
		filepath.Join("state", "record.json"): "[]\n",
	}

	for name, content := range placeholders {
		p := filepath.Join(path, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			return fmt.Errorf("creating %s: %w", name, err)
		}
	}

	return nil
}

// GroupContent returns the content of the group heading file (tasks/{group}/group.md).
// Returns empty string if the group is empty or the file doesn't exist.
func (d *Dir) GroupContent(group string) (string, error) {
	if group == "" {
		return "", nil
	}
	return d.readFile(filepath.Join("tasks", group, "group.md"))
}

// MissionPreamble is prepended to every assembled document to keep Claude focused on the task.
const MissionPreamble = `# Mission

Your sole objective is to implement the task described in the "Task" section below. Every action you take — reading files, writing code, running commands — must directly serve that task. Do not make changes unrelated to the task, do not refactor surrounding code, do not "improve" things you notice along the way. If the task says to add a feature, add exactly that feature. If it says to fix a bug, fix exactly that bug. Stay focused.

`

// AssembleDocument builds a single markdown document from rules, lint, group heading, task content, and functional specs.
// The groupContent parameter is included as a "# Group" section between lint and task if non-empty.
func (d *Dir) AssembleDocument(taskContent, groupContent string) (string, error) {
	rules, err := d.Rules()
	if err != nil {
		return "", err
	}

	lint, err := d.Lint()
	if err != nil {
		return "", err
	}

	functional, err := d.Functional()
	if err != nil {
		return "", err
	}

	doc := MissionPreamble
	if rules != "" {
		doc += "# Rules\n\n" + rules + "\n\n"
	}
	if lint != "" {
		doc += "# Lint Rules\n\n" + lint + "\n\n"
	}
	if groupContent != "" {
		doc += "# Group\n\n" + groupContent + "\n\n"
	}
	doc += "# Task\n\n" + taskContent + "\n\n"
	if functional != "" {
		doc += "# Functional Tests\n\n" + functional + "\n\n"
	}

	return doc, nil
}
