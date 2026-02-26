// Package taskrun loads and executes named commands from a hydra.yml config.
package taskrun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Commands holds the named commands loaded from hydra.yml.
type Commands struct {
	Commands map[string]string `yaml:"commands"`
}

// Load reads and parses a hydra.yml file.
func Load(path string) (*Commands, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path constructed from trusted design dir
	if err != nil {
		return nil, fmt.Errorf("reading taskrun config: %w", err)
	}

	var cmds Commands
	if err := yaml.Unmarshal(data, &cmds); err != nil {
		return nil, fmt.Errorf("parsing taskrun config: %w", err)
	}

	if cmds.Commands == nil {
		cmds.Commands = make(map[string]string)
	}

	return &cmds, nil
}

// Run executes the named command in the given working directory.
// Returns nil if the command name is not defined.
func (c *Commands) Run(name, workDir string) error {
	cmdStr, ok := c.Commands[name]
	if !ok {
		return nil
	}

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return nil
	}

	cmd := exec.CommandContext(context.Background(), parts[0], parts[1:]...) //nolint:gosec // commands from trusted config
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command %q failed: %w", name, err)
	}

	return nil
}
