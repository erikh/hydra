// Package cmd defines the hydra CLI commands.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/issues"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
	"github.com/erikh/hydra/internal/runner"
	"github.com/erikh/hydra/internal/taskrun"
	"github.com/erikh/hydra/internal/tui"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
	"go.yaml.in/yaml/v4"
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
			otherCommand(),
			reviewCommand(),
			testCommand(),
			cleanCommand(),
			mergeCommand(),
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
		Usage:     "Create or edit a task in the design directory",
		ArgsUsage: "<task-name>",
		Description: "Opens your editor to create or edit a task file. If the task already " +
			"exists, opens it in-place. The editor is resolved from $VISUAL, then $EDITOR. " +
			"The task name must not contain '/'.",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra edit <task-name>")
			}

			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config (are you in an initialized hydra directory?): %w", err)
			}

			editor, err := resolveEditor()
			if err != nil {
				return err
			}

			taskName := c.Args().Get(0)
			return design.EditTask(cfg.DesignDir, taskName, editor, os.Stdin, os.Stdout, os.Stderr)
		},
	}
}

func resolveEditor() (string, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return "", errors.New("no editor configured: set $VISUAL or $EDITOR")
	}
	return editor, nil
}

func otherCommand() *cli.Command {
	return &cli.Command{
		Name:  "other",
		Usage: "Manage miscellaneous files in the other/ directory",
		Description: "CRUD operations for files in the design directory's other/ folder. " +
			"These are supporting documents that aren't tasks.",
		Subcommands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List files in other/",
				Action: func(_ *cli.Context) error {
					cfg, err := config.Discover()
					if err != nil {
						return fmt.Errorf("loading config: %w", err)
					}
					dd, err := design.NewDir(cfg.DesignDir)
					if err != nil {
						return err
					}
					files, err := dd.OtherFiles()
					if err != nil {
						return err
					}
					if len(files) == 0 {
						fmt.Println("No files in other/.")
						return nil
					}
					for _, f := range files {
						fmt.Println(f)
					}
					return nil
				},
			},
			{
				Name:      "add",
				Usage:     "Create a new file in other/",
				ArgsUsage: "<name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra other add <name>")
					}
					cfg, err := config.Discover()
					if err != nil {
						return fmt.Errorf("loading config: %w", err)
					}
					editor, err := resolveEditor()
					if err != nil {
						return err
					}
					return design.AddOtherFile(cfg.DesignDir, c.Args().Get(0), editor, os.Stdin, os.Stdout, os.Stderr)
				},
			},
			{
				Name:      "view",
				Usage:     "Print the content of a file in other/",
				ArgsUsage: "<name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra other view <name>")
					}
					cfg, err := config.Discover()
					if err != nil {
						return fmt.Errorf("loading config: %w", err)
					}
					dd, err := design.NewDir(cfg.DesignDir)
					if err != nil {
						return err
					}
					content, err := dd.OtherContent(c.Args().Get(0))
					if err != nil {
						return err
					}
					fmt.Print(content)
					return nil
				},
			},
			{
				Name:      "edit",
				Usage:     "Edit an existing file in other/",
				ArgsUsage: "<name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra other edit <name>")
					}
					cfg, err := config.Discover()
					if err != nil {
						return fmt.Errorf("loading config: %w", err)
					}
					editor, err := resolveEditor()
					if err != nil {
						return err
					}
					return design.EditOtherFile(cfg.DesignDir, c.Args().Get(0), editor, os.Stdin, os.Stdout, os.Stderr)
				},
			},
			{
				Name:      "rm",
				Usage:     "Remove a file from other/",
				ArgsUsage: "<name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra other rm <name>")
					}
					cfg, err := config.Discover()
					if err != nil {
						return fmt.Errorf("loading config: %w", err)
					}
					dd, err := design.NewDir(cfg.DesignDir)
					if err != nil {
						return err
					}
					return dd.RemoveOtherFile(c.Args().Get(0))
				},
			},
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
				Name:    "no-auto-accept",
				Aliases: []string{"Y"},
				Usage:   "Disable auto-accept (prompt for each tool call)",
			},
			&cli.BoolFlag{
				Name:    "no-plan",
				Aliases: []string{"P"},
				Usage:   "Disable plan mode (skip plan approval, run fully autonomously)",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Override the Claude model",
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

			r.AutoAccept = true
			r.PlanMode = true
			if c.Bool("no-auto-accept") {
				r.AutoAccept = false
			}
			if c.Bool("no-plan") {
				r.PlanMode = false
			}
			if m := c.String("model"); m != "" {
				r.Model = m
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
				Name:    "no-auto-accept",
				Aliases: []string{"Y"},
				Usage:   "Disable auto-accept (prompt for each tool call)",
			},
			&cli.BoolFlag{
				Name:    "no-plan",
				Aliases: []string{"P"},
				Usage:   "Disable plan mode (skip plan approval, run fully autonomously)",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Override the Claude model",
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

			r.AutoAccept = true
			r.PlanMode = true
			if c.Bool("no-auto-accept") {
				r.AutoAccept = false
			}
			if c.Bool("no-plan") {
				r.PlanMode = false
			}
			if m := c.String("model"); m != "" {
				r.Model = m
			}

			return r.RunGroup(c.Args().Get(0))
		},
	}
}

type statusRunning struct {
	Action string `json:"action" yaml:"action"`
	PID    int    `json:"pid" yaml:"pid"`
}

type statusOutput struct {
	Running   map[string]statusRunning `json:"running,omitempty" yaml:"running,omitempty"`
	Pending   []string                 `json:"pending,omitempty" yaml:"pending,omitempty"`
	Review    []string                 `json:"review,omitempty" yaml:"review,omitempty"`
	Merge     []string                 `json:"merge,omitempty" yaml:"merge,omitempty"`
	Completed []string                 `json:"completed,omitempty" yaml:"completed,omitempty"`
	Abandoned []string                 `json:"abandoned,omitempty" yaml:"abandoned,omitempty"`
}

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show task states and running tasks as YAML (or JSON with -j)",
		Description: "Outputs a structured document with tasks grouped by state " +
			"(running, pending, review, merge, completed, abandoned). " +
			"Running tasks are keyed by name with 'action' and 'pid' fields. " +
			"Default format is YAML; pass -j/--json for JSON.\n\n" +
			"When stdout is a TTY, output is syntax-highlighted. Colors are " +
			"sourced from pywal (~/.cache/wal/colors.json) when available, " +
			"otherwise a built-in theme is used. Pass --no-color to disable.\n\n" +
			"Example YAML output:\n\n" +
			"  running:\n" +
			"    my-task:\n" +
			"      action: reviewing\n" +
			"      pid: 12345\n" +
			"  pending:\n" +
			"    - other-task\n" +
			"  review:\n" +
			"    - done-task",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output as JSON instead of YAML",
			},
			&cli.BoolFlag{
				Name:  "no-color",
				Usage: "Disable syntax highlighting",
			},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dd, err := design.NewDir(cfg.DesignDir)
			if err != nil {
				return err
			}

			var out statusOutput

			// Collect running tasks.
			runningSet := make(map[string]bool)
			running, err := lock.ReadAll(config.HydraPath("."))
			if err == nil && len(running) > 0 {
				out.Running = make(map[string]statusRunning, len(running))
				for _, rt := range running {
					action, name := parseRunningTask(rt.TaskName)
					out.Running[name] = statusRunning{
						Action: action,
						PID:    rt.PID,
					}
					runningSet[rt.TaskName] = true
				}
			}

			// Collect tasks by state.
			stateSlices := []struct {
				state design.TaskState
				dest  *[]string
			}{
				{design.StatePending, &out.Pending},
				{design.StateReview, &out.Review},
				{design.StateMerge, &out.Merge},
				{design.StateCompleted, &out.Completed},
				{design.StateAbandoned, &out.Abandoned},
			}
			for _, ss := range stateSlices {
				tasks, err := dd.TasksByState(ss.state)
				if err != nil {
					return err
				}
				for _, t := range tasks {
					label := t.Name
					if t.Group != "" {
						label = t.Group + "/" + t.Name
					}
					if ss.state == design.StatePending && runningSet[label] {
						continue
					}
					*ss.dest = append(*ss.dest, label)
				}
			}

			var buf bytes.Buffer
			lang := "yaml"
			if c.Bool("json") {
				lang = "json"
				enc := json.NewEncoder(&buf)
				enc.SetIndent("", "  ")
				if err := enc.Encode(out); err != nil {
					return err
				}
			} else {
				if err := yaml.NewEncoder(&buf).Encode(out); err != nil {
					return err
				}
			}

			if !c.Bool("no-color") && isatty.IsTerminal(os.Stdout.Fd()) {
				lexer := lexers.Get(lang)
				if lexer == nil {
					lexer = lexers.Fallback
				}
				lexer = chroma.Coalesce(lexer)
				formatter := formatters.Get("terminal256")
				style := tui.LoadTheme().ChromaStyle()
				iterator, err := lexer.Tokenise(nil, buf.String())
				if err != nil {
					return err
				}
				return formatter.Format(os.Stdout, style, iterator)
			}
			_, err = buf.WriteTo(os.Stdout)
			return err
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
			apiType := ""
			giteaURL := ""
			if cmds != nil {
				apiType = cmds.APIType
				giteaURL = cmds.GiteaURL
			}
			source, err := issues.ResolveSource(cfg.SourceRepoURL, apiType, giteaURL)
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

// stateOps holds the per-state runner operations used by stateCommand.
type stateOps struct {
	list func(r *runner.Runner) error
	view func(r *runner.Runner, name string) error
	edit func(r *runner.Runner, name, editor string) error
	rm   func(r *runner.Runner, name string) error
	run  func(r *runner.Runner, name string) error
}

// stateCommand builds a CLI command with list/view/edit/rm/run subcommands
// for a given task state (review, merge, etc.).
func stateCommand(name, usage, description, runUsage string, ops stateOps) *cli.Command {
	return &cli.Command{
		Name:        name,
		Usage:       usage,
		Description: description,
		Subcommands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List tasks in " + name + " state",
				Action: func(_ *cli.Context) error {
					r, err := newRunner()
					if err != nil {
						return err
					}
					return ops.list(r)
				},
			},
			{
				Name:      "view",
				Usage:     "Print task content from " + name + " state",
				ArgsUsage: "<task-name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return fmt.Errorf("usage: hydra %s view <task-name>", name)
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					return ops.view(r, c.Args().Get(0))
				},
			},
			{
				Name:      "edit",
				Usage:     "Open a task in " + name + " state in the editor",
				ArgsUsage: "<task-name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return fmt.Errorf("usage: hydra %s edit <task-name>", name)
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					editor, err := resolveEditor()
					if err != nil {
						return err
					}
					return ops.edit(r, c.Args().Get(0), editor)
				},
			},
			{
				Name:      "rm",
				Usage:     "Move a task from " + name + " to abandoned",
				ArgsUsage: "<task-name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return fmt.Errorf("usage: hydra %s rm <task-name>", name)
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					return ops.rm(r, c.Args().Get(0))
				},
			},
			{
				Name:      "run",
				Usage:     runUsage,
				ArgsUsage: "<task-name>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "no-auto-accept",
						Aliases: []string{"Y"},
						Usage:   "Disable auto-accept (prompt for each tool call)",
					},
					&cli.BoolFlag{
						Name:    "no-plan",
						Aliases: []string{"P"},
						Usage:   "Disable plan mode (skip plan approval, run fully autonomously)",
					},
					&cli.StringFlag{
						Name:  "model",
						Usage: "Override the Claude model",
					},
				},
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return fmt.Errorf("usage: hydra %s run <task-name>", name)
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					r.AutoAccept = true
					r.PlanMode = true
					if c.Bool("no-auto-accept") {
						r.AutoAccept = false
					}
					if c.Bool("no-plan") {
						r.PlanMode = false
					}
					if m := c.String("model"); m != "" {
						r.Model = m
					}
					return ops.run(r, c.Args().Get(0))
				},
			},
		},
	}
}

// newRunner creates a runner from discovered config.
func newRunner() (*runner.Runner, error) {
	cfg, err := config.Discover()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return runner.New(cfg)
}

func reviewCommand() *cli.Command {
	return &cli.Command{
		Name:        "review",
		Usage:       "Manage and run review sessions on completed tasks",
		Description: "CRUD operations and interactive review sessions for tasks in the review state.",
		Subcommands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List tasks in review state",
				Action: func(_ *cli.Context) error {
					r, err := newRunner()
					if err != nil {
						return err
					}
					return r.ReviewList()
				},
			},
			{
				Name:      "view",
				Usage:     "Print task content from review state",
				ArgsUsage: "<task-name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra review view <task-name>")
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					return r.ReviewView(c.Args().Get(0))
				},
			},
			{
				Name:      "edit",
				Usage:     "Open a task in review state in the editor",
				ArgsUsage: "<task-name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra review edit <task-name>")
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					editor, err := resolveEditor()
					if err != nil {
						return err
					}
					return r.ReviewEdit(c.Args().Get(0), editor)
				},
			},
			{
				Name:      "rm",
				Usage:     "Move a task from review to abandoned",
				ArgsUsage: "<task-name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra review rm <task-name>")
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					return r.ReviewRemove(c.Args().Get(0))
				},
			},
			{
				Name:      "run",
				Usage:     "Run an interactive review session",
				ArgsUsage: "<task-name>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "no-auto-accept",
						Aliases: []string{"Y"},
						Usage:   "Disable auto-accept (prompt for each tool call)",
					},
					&cli.BoolFlag{
						Name:    "no-plan",
						Aliases: []string{"P"},
						Usage:   "Disable plan mode (skip plan approval, run fully autonomously)",
					},
					&cli.StringFlag{
						Name:  "model",
						Usage: "Override the Claude model",
					},
				},
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra review run <task-name>")
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					r.AutoAccept = true
					r.PlanMode = true
					if c.Bool("no-auto-accept") {
						r.AutoAccept = false
					}
					if c.Bool("no-plan") {
						r.PlanMode = false
					}
					if m := c.String("model"); m != "" {
						r.Model = m
					}
					return r.Review(c.Args().Get(0))
				},
			},
			{
				Name:      "dev",
				Usage:     "Run the dev command from hydra.yml in the task's work directory",
				ArgsUsage: "<task-name>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra review dev <task-name>")
					}

					ctx, stop := signal.NotifyContext(context.Background(),
						syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
					defer stop()

					r, err := newRunner()
					if err != nil {
						return err
					}
					return r.ReviewDev(ctx, c.Args().Get(0))
				},
			},
		},
	}
}

func testCommand() *cli.Command {
	return &cli.Command{
		Name:      "test",
		Usage:     "Add tests for a task in review state",
		ArgsUsage: "<task-name>",
		Description: "Opens a Claude session that reads the task description, adds missing tests, " +
			"runs test and lint commands from hydra.yml, and fixes any issues. " +
			"The task stays in review state after the session.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "no-auto-accept",
				Aliases: []string{"Y"},
				Usage:   "Disable auto-accept (prompt for each tool call)",
			},
			&cli.BoolFlag{
				Name:    "no-plan",
				Aliases: []string{"P"},
				Usage:   "Disable plan mode (skip plan approval, run fully autonomously)",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Override the Claude model",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra test <task-name>")
			}

			r, err := newRunner()
			if err != nil {
				return err
			}

			r.AutoAccept = true
			r.PlanMode = true
			if c.Bool("no-auto-accept") {
				r.AutoAccept = false
			}
			if c.Bool("no-plan") {
				r.PlanMode = false
			}
			if m := c.String("model"); m != "" {
				r.Model = m
			}

			return r.Test(c.Args().Get(0))
		},
	}
}

func cleanCommand() *cli.Command {
	return &cli.Command{
		Name:      "clean",
		Usage:     "Run the clean command from hydra.yml in a task's work directory",
		ArgsUsage: "<task-name>",
		Description: "Runs the clean command defined in hydra.yml in the task's work directory, " +
			"regardless of which state the task is in.",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra clean <task-name>")
			}

			r, err := newRunner()
			if err != nil {
				return err
			}

			return r.Clean(c.Args().Get(0))
		},
	}
}

func mergeCommand() *cli.Command {
	return stateCommand(
		"merge",
		"Manage and run merge workflows on reviewed tasks",
		"CRUD operations and merge workflow for tasks in review or merge state.",
		"Run the merge workflow (rebase, test, merge, push)",
		stateOps{
			list: (*runner.Runner).MergeList,
			view: (*runner.Runner).MergeView,
			edit: (*runner.Runner).MergeEdit,
			rm:   (*runner.Runner).MergeRemove,
			run:  (*runner.Runner).Merge,
		},
	)
}

// parseRunningTask splits a raw lock name like "review:foo" into
// a display state ("reviewing") and the task name ("foo").
func parseRunningTask(name string) (state, task string) {
	labels := map[string]string{
		"review": "reviewing",
		"merge":  "merging",
		"test":   "testing",
	}

	if prefix, rest, ok := strings.Cut(name, ":"); ok {
		if label, found := labels[prefix]; found {
			return label, rest
		}
	}
	return "running", name
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
