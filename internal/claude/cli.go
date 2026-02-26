package claude

import (
	"context"
	"os"
	"os/exec"
)

// CLIConfig configures a Claude Code CLI invocation.
type CLIConfig struct {
	CLIPath    string
	Prompt     string
	Model      string
	WorkDir    string
	AutoAccept bool
	PlanMode   bool
}

// FindCLI looks for the `claude` binary on PATH.
// Returns the path or empty string if not found.
func FindCLI() string {
	path, err := exec.LookPath("claude")
	if err != nil {
		return ""
	}
	return path
}

// BuildArgs constructs the argument list for the claude CLI from the config.
// The prompt is passed as a positional argument so claude runs interactively.
func BuildArgs(cfg CLIConfig) []string {
	var args []string

	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}

	if cfg.AutoAccept {
		args = append(args, "--dangerously-skip-permissions")
	}

	if cfg.PlanMode {
		args = append(args, "--plan")
	}

	// Positional argument: starts an interactive session with this prompt.
	args = append(args, cfg.Prompt)

	return args
}

// RunCLI invokes the claude CLI as a subprocess with the given config.
// The process inherits stdin/stdout/stderr for interactive use.
func RunCLI(ctx context.Context, cfg CLIConfig) error {
	args := BuildArgs(cfg)

	cmd := exec.CommandContext(ctx, cfg.CLIPath, args...) //nolint:gosec // CLIPath comes from exec.LookPath, not user input
	cmd.Dir = cfg.WorkDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
