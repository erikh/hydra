// Package cmd defines the hydra CLI commands.
package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
	"github.com/erikh/hydra/internal/runner"
	"github.com/urfave/cli/v2"
)

// NewApp creates the hydra CLI application.
func NewApp() *cli.App {
	return &cli.App{
		Name:  "hydra",
		Usage: "Drive development tasks from design documents",
		Description: "Hydra drives development tasks from a tree of markdown design documents " +
			"against a source code repository. It integrates with the Claude CLI to execute " +
			"design tasks and manage the full development lifecycle including testing, linting, " +
			"and commit tracking.",
		Commands: []*cli.Command{
			initCommand(),
			runCommand(),
			statusCommand(),
			listCommand(),
			milestoneCommand(),
		},
	}
}

func initCommand() *cli.Command {
	return &cli.Command{
		Name:      "init",
		Usage:     "Initialize a hydra project",
		ArgsUsage: "<source-repo-url> <design-dir>",
		Description: "Clones the source repository and registers the design directory. " +
			"If the design directory is empty, creates the full skeleton structure including " +
			"tasks/, state/, milestone/, and configuration files.",
		Action: func(c *cli.Context) error {
			if c.NArg() != 2 {
				return errors.New("usage: hydra init <source-repo-url> <design-dir>")
			}

			sourceURL := c.Args().Get(0)
			designDir := c.Args().Get(1)

			// Ensure design dir exists (create if needed).
			if err := os.MkdirAll(designDir, 0o750); err != nil {
				return fmt.Errorf("creating design dir %q: %w", designDir, err)
			}

			// Scaffold the design directory if it doesn't have content yet.
			if err := design.Scaffold(designDir); err != nil {
				return fmt.Errorf("scaffolding design dir: %w", err)
			}

			// Validate design dir exists.
			info, err := os.Stat(designDir)
			if err != nil {
				return fmt.Errorf("design dir %q: %w", designDir, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("%q is not a directory", designDir)
			}

			cfg, err := config.Init(".", sourceURL, designDir)
			if err != nil {
				return err
			}

			// Clone the source repo
			fmt.Printf("Cloning %s...\n", sourceURL)
			if _, err := repo.Clone(sourceURL, cfg.RepoDir); err != nil {
				return err
			}

			fmt.Println("Initialized hydra project.")
			fmt.Printf("  Source repo: %s\n", cfg.RepoDir)
			fmt.Printf("  Design dir:  %s\n", cfg.DesignDir)
			return nil
		},
	}
}

func runCommand() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Execute a design task",
		ArgsUsage: "<task-name>",
		Description: "Executes the full task lifecycle: acquires a lock, creates a git branch, " +
			"assembles the design document, invokes Claude, runs tests and linter, commits, " +
			"pushes, records the commit SHA, and moves the task to review.",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra run <task-name>")
			}

			cfg, err := config.Load(".")
			if err != nil {
				return fmt.Errorf("loading config (are you in an initialized hydra directory?): %w", err)
			}

			r, err := runner.New(cfg)
			if err != nil {
				return err
			}

			return r.Run(c.Args().Get(0))
		},
	}
}

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show task states and current running task",
		Description: "Displays tasks grouped by state (pending, review, merge, completed, abandoned) " +
			"and shows any currently running task with its PID.",
		Action: func(_ *cli.Context) error {
			cfg, err := config.Load(".")
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dd, err := design.NewDir(cfg.DesignDir)
			if err != nil {
				return err
			}

			// Show current running task
			taskName, pid, err := lock.ReadCurrent(config.HydraPath("."))
			if err == nil {
				fmt.Printf("Running: %s (PID %d)\n\n", taskName, pid)
			}

			// Show tasks by state
			for _, state := range []design.TaskState{
				design.StatePending,
				design.StateReview,
				design.StateMerge,
				design.StateCompleted,
				design.StateAbandoned,
			} {
				tasks, err := dd.TasksByState(state)
				if err != nil {
					return err
				}
				if len(tasks) == 0 {
					continue
				}

				fmt.Printf("%s:\n", state)
				for _, t := range tasks {
					label := t.Name
					if t.Group != "" {
						label = t.Group + "/" + t.Name
					}
					fmt.Printf("  - %s\n", label)
				}
				fmt.Println()
			}

			return nil
		},
	}
}

func listCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List available pending tasks",
		Description: "Shows all pending tasks from the design directory's tasks/ folder, " +
			"including grouped tasks displayed as group/name.",
		Action: func(_ *cli.Context) error {
			cfg, err := config.Load(".")
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dd, err := design.NewDir(cfg.DesignDir)
			if err != nil {
				return err
			}

			tasks, err := dd.PendingTasks()
			if err != nil {
				return err
			}

			if len(tasks) == 0 {
				fmt.Println("No pending tasks.")
				return nil
			}

			for _, t := range tasks {
				label := t.Name
				if t.Group != "" {
					label = t.Group + "/" + t.Name
				}
				fmt.Println(label)
			}

			return nil
		},
	}
}

func milestoneCommand() *cli.Command {
	return &cli.Command{
		Name:  "milestone",
		Usage: "List milestone targets and historical scores",
		Description: "Lists milestone targets from the milestone/ directory and historical " +
			"milestone scores from milestone/history/. Milestones are date-based markdown " +
			"files; history entries include a letter grade (A-F).",
		Action: func(_ *cli.Context) error {
			cfg, err := config.Load(".")
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dd, err := design.NewDir(cfg.DesignDir)
			if err != nil {
				return err
			}

			milestones, err := dd.Milestones()
			if err != nil {
				return err
			}

			if len(milestones) > 0 {
				fmt.Println("Milestones:")
				for _, m := range milestones {
					fmt.Printf("  - %s\n", m.Date)
				}
				fmt.Println()
			}

			history, err := dd.MilestoneHistory()
			if err != nil {
				return err
			}

			if len(history) > 0 {
				fmt.Println("History:")
				for _, h := range history {
					fmt.Printf("  - %s [%s]\n", h.Date, h.Score)
				}
				fmt.Println()
			}

			if len(milestones) == 0 && len(history) == 0 {
				fmt.Println("No milestones found.")
			}

			return nil
		},
	}
}
