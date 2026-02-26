// Package cmd defines the hydra CLI commands.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/issues"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
	"github.com/erikh/hydra/internal/runner"
	"github.com/erikh/hydra/internal/taskrun"
	"github.com/urfave/cli/v2"
)

// NewApp creates the hydra CLI application.
func NewApp() *cli.App {
	return &cli.App{
		Name:  "hydra",
		Usage: "Local pull request workflow where Claude is the only contributor",
		Description: "Hydra turns markdown design documents into branches, code, and commits. " +
			"It assembles context from your design docs, hands it to Claude, runs tests and " +
			"linting, and pushes a branch ready for your review.",
		Commands: []*cli.Command{
			initCommand(),
			runCommand(),
			runGroupCommand(),
			editCommand(),
			statusCommand(),
			listCommand(),
			milestoneCommand(),
			syncCommand(),
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

			// Create a convenience symlink at ./design pointing to the design dir.
			symlink := filepath.Join(".", "design")
			if _, err := os.Lstat(symlink); os.IsNotExist(err) {
				if err := os.Symlink(cfg.DesignDir, symlink); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not create design symlink: %v\n", err)
				}
			}

			fmt.Println("Initialized hydra project.")
			fmt.Printf("  Source repo: %s\n", cfg.RepoDir)
			fmt.Printf("  Design dir:  %s\n", cfg.DesignDir)
			return nil
		},
	}
}

func editCommand() *cli.Command {
	return &cli.Command{
		Name:      "edit",
		Usage:     "Create a new task in the design directory",
		ArgsUsage: "<task-name>",
		Description: "Opens your editor to create a new task file. The editor is resolved " +
			"from $VISUAL, then $EDITOR. The task name must not contain '/'.",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra edit <task-name>")
			}

			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config (are you in an initialized hydra directory?): %w", err)
			}

			editor := os.Getenv("VISUAL")
			if editor == "" {
				editor = os.Getenv("EDITOR")
			}
			if editor == "" {
				return errors.New("no editor configured: set $VISUAL or $EDITOR")
			}

			taskName := c.Args().Get(0)
			return design.EditNewTask(cfg.DesignDir, taskName, editor, os.Stdin, os.Stdout, os.Stderr)
		},
	}
}

func runCommand() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Execute a design task",
		ArgsUsage: "<task-name>",
		Description: "Executes the full task lifecycle: acquires a lock, creates a git branch, " +
			"assembles the design document, invokes Claude via the Anthropic API with an " +
			"interactive TUI, runs tests and linter, commits, pushes, records the commit SHA, " +
			"and moves the task to review.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "auto-accept",
				Aliases: []string{"y"},
				Usage:   "Auto-accept all tool calls without prompting",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra run <task-name>")
			}

			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config (are you in an initialized hydra directory?): %w", err)
			}

			r, err := runner.New(cfg)
			if err != nil {
				return err
			}

			if c.Bool("auto-accept") {
				r.AutoAccept = true
			}

			return r.Run(c.Args().Get(0))
		},
	}
}

func runGroupCommand() *cli.Command {
	return &cli.Command{
		Name:      "run-group",
		Usage:     "Execute all pending tasks in a group sequentially",
		ArgsUsage: "<group-name>",
		Description: "Runs all pending tasks in the named group in alphabetical order. " +
			"Between tasks, the base branch is restored so each task starts from a clean state. " +
			"Stops immediately on the first error.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "auto-accept",
				Aliases: []string{"y"},
				Usage:   "Auto-accept all tool calls without prompting",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra run-group <group-name>")
			}

			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config (are you in an initialized hydra directory?): %w", err)
			}

			r, err := runner.New(cfg)
			if err != nil {
				return err
			}

			if c.Bool("auto-accept") {
				r.AutoAccept = true
			}

			return r.RunGroup(c.Args().Get(0))
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
			cfg, err := config.Discover()
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
			cfg, err := config.Discover()
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

func syncCommand() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Import open issues from GitHub or Gitea as design tasks",
		Description: "Fetches open issues from the source repository's issue tracker and " +
			"creates task files under tasks/issues/. Existing issues (matched by number) " +
			"are skipped. Supports both GitHub and Gitea; the API type is auto-detected " +
			"from the remote URL or can be set via api_type in hydra.yml.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "label",
				Usage: "Filter issues by label (can be specified multiple times)",
			},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config (are you in an initialized hydra directory?): %w", err)
			}

			labels := c.StringSlice("label")

			// Load hydra.yml for api_type/gitea_url overrides.
			var cmds *taskrun.Commands
			ymlPath := filepath.Join(cfg.DesignDir, "hydra.yml")
			if _, err := os.Stat(ymlPath); err == nil {
				cmds, err = taskrun.Load(ymlPath)
				if err != nil {
					return fmt.Errorf("loading hydra.yml: %w", err)
				}
			}

			// Determine the source.
			source, err := resolveIssueSource(cfg.SourceRepoURL, cmds)
			if err != nil {
				return err
			}

			created, skipped, err := issues.Sync(context.Background(), cfg.DesignDir, source, labels)
			if err != nil {
				return err
			}

			fmt.Printf("Synced issues: %d created, %d skipped\n", created, skipped)
			return nil
		},
	}
}

func resolveIssueSource(repoURL string, cmds *taskrun.Commands) (issues.Source, error) {
	apiType := ""
	giteaURL := ""
	giteaToken := ""
	if cmds != nil {
		apiType = cmds.APIType
		giteaURL = cmds.GiteaURL
	}

	// Explicit api_type override.
	if apiType == "github" {
		owner, repo, ok := issues.ParseGitHubURL(repoURL)
		if !ok {
			return nil, fmt.Errorf("cannot parse GitHub owner/repo from %q", repoURL)
		}
		return issues.NewGitHubSource(owner, repo), nil
	}
	if apiType == "gitea" {
		baseURL := giteaURL
		if baseURL == "" {
			var owner, repo string
			var ok bool
			baseURL, owner, repo, ok = issues.ParseGiteaURL(repoURL)
			if !ok {
				return nil, fmt.Errorf("cannot parse Gitea URL from %q", repoURL)
			}
			return issues.NewGiteaSource(baseURL, owner, repo, giteaToken), nil
		}
		// Parse owner/repo from URL even when base URL is overridden.
		_, owner, repo, ok := issues.ParseGiteaURL(repoURL)
		if !ok {
			return nil, fmt.Errorf("cannot parse owner/repo from %q", repoURL)
		}
		return issues.NewGiteaSource(baseURL, owner, repo, giteaToken), nil
	}

	// Auto-detect: if URL contains github.com, use GitHub.
	if strings.Contains(repoURL, "github.com") {
		owner, repo, ok := issues.ParseGitHubURL(repoURL)
		if !ok {
			return nil, fmt.Errorf("cannot parse GitHub owner/repo from %q", repoURL)
		}
		return issues.NewGitHubSource(owner, repo), nil
	}

	// Default to Gitea for non-GitHub hosts.
	baseURL, owner, repo, ok := issues.ParseGiteaURL(repoURL)
	if !ok {
		return nil, fmt.Errorf("cannot determine issue source from %q; set api_type in hydra.yml", repoURL)
	}
	return issues.NewGiteaSource(baseURL, owner, repo, giteaToken), nil
}

func milestoneCommand() *cli.Command {
	return &cli.Command{
		Name:  "milestone",
		Usage: "List milestone targets and historical scores",
		Description: "Lists milestone targets from the milestone/ directory and historical " +
			"milestone scores from milestone/history/. Milestones are date-based markdown " +
			"files; history entries include a letter grade (A-F).",
		Action: func(_ *cli.Context) error {
			cfg, err := config.Discover()
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
