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

// AssembleDocument builds a single markdown document from rules, lint, task content, and functional specs.
func (d *Dir) AssembleDocument(taskContent string) (string, error) {
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

	doc := ""
	if rules != "" {
		doc += "# Rules\n\n" + rules + "\n\n"
	}
	if lint != "" {
		doc += "# Lint Rules\n\n" + lint + "\n\n"
	}
	doc += "# Task\n\n" + taskContent + "\n\n"
	if functional != "" {
		doc += "# Functional Tests\n\n" + functional + "\n\n"
	}

	return doc, nil
}
