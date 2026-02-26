package design

import (
	"fmt"
	"os"
	"path/filepath"
)

type DesignDir struct {
	Path string
}

func NewDesignDir(path string) (*DesignDir, error) {
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

	return &DesignDir{Path: abs}, nil
}

func (d *DesignDir) readFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(d.Path, name))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", name, err)
	}
	return string(data), nil
}

func (d *DesignDir) Rules() (string, error) {
	return d.readFile("rules.md")
}

func (d *DesignDir) Lint() (string, error) {
	return d.readFile("lint.md")
}

func (d *DesignDir) Functional() (string, error) {
	return d.readFile("functional.md")
}

func (d *DesignDir) AssembleDocument(taskContent string) (string, error) {
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
