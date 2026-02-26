// Package taskrun loads and executes named commands from a hydra.yml config.
package taskrun

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Commands holds the named commands loaded from hydra.yml.
type Commands struct {
	Model    string            `yaml:"model"`
	APIType  string            `yaml:"api_type"`
	GiteaURL string            `yaml:"gitea_url"`
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

// hasMakeTarget checks if a Makefile exists in workDir and contains the given target.
func hasMakeTarget(workDir, target string) bool {
	makefile := filepath.Join(workDir, "Makefile")
	data, err := os.ReadFile(makefile) //nolint:gosec // workDir is a trusted path
	if err != nil {
		return false
	}
	// Look for a line starting with "target:" (make rule syntax).
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, target+":") {
			return true
		}
	}
	return false
}

// resolveCommand returns the command string for the given name.
// It checks hydra.yml first, then falls back to "make <name>" if a Makefile
// with that target exists in the work directory.
func (c *Commands) resolveCommand(name, workDir string) (string, bool) {
	if cmdStr, ok := c.Commands[name]; ok {
		return cmdStr, true
	}
	if hasMakeTarget(workDir, name) {
		return "make " + name, true
	}
	return "", false
}

// RunDev executes the named "dev" command in the given working directory.
// The command runs until it exits or the context is cancelled.
// Falls back to "make dev" if no dev command is configured but a Makefile
// with a dev target exists. Returns an error if neither is available.
func (c *Commands) RunDev(ctx context.Context, workDir string) error {
	cmdStr, ok := c.resolveCommand("dev", workDir)
	if !ok {
		return errors.New("no dev command configured in hydra.yml and no dev target in Makefile")
	}

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return errors.New("dev command is empty in hydra.yml")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...) //nolint:gosec // commands from trusted config
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dev command failed: %w", err)
	}

	return nil
}

// EffectiveCommands returns the commands map including Makefile fallbacks.
// For each standard command name (clean, dev, test, lint) not configured in
// hydra.yml, if a matching Makefile target exists in workDir, it is included
// as "make <name>".
func (c *Commands) EffectiveCommands(workDir string) map[string]string {
	result := make(map[string]string)
	maps.Copy(result, c.Commands)
	for _, name := range []string{"clean", "dev", "test", "lint"} {
		if _, ok := result[name]; !ok {
			if hasMakeTarget(workDir, name) {
				result[name] = "make " + name
			}
		}
	}
	return result
}

// HasCommand reports whether a command is available for the given name,
// either from hydra.yml or via a Makefile target in workDir.
func (c *Commands) HasCommand(name, workDir string) bool {
	_, ok := c.resolveCommand(name, workDir)
	return ok
}

// Run executes the named command in the given working directory.
// Falls back to "make <name>" if the command is not configured in hydra.yml
// but a Makefile with that target exists. Returns nil if neither is available.
func (c *Commands) Run(name, workDir string) error {
	cmdStr, ok := c.resolveCommand(name, workDir)
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
