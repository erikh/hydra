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
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/notify"
	"github.com/erikh/hydra/internal/repo"
	"github.com/erikh/hydra/internal/runner"
	"github.com/erikh/hydra/internal/tui"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
	"go.yaml.in/yaml/v4"
)

// NewApp creates the hydra CLI application.
func NewApp() *cli.App {
	return &cli.App{
		Name:                 "hydra",
		Usage:                "Local pull request workflow where Claude is the only contributor",
		EnableBashCompletion: true,
		Description: "Hydra turns markdown design documents into branches, code, and commits. " +
			"It assembles context from your design docs, hands it to Claude, runs tests and " +
			"linting, and pushes a branch ready for your review.",
		Before: func(c *cli.Context) error {
			if c.Args().First() != "completion" {
				promptCompletionInstall()
			}
			setTerminalTitle(c)
			return nil
		},
		Commands: []*cli.Command{
			initCommand(),
			runCommand(),
			groupCommand(),
			editCommand(),
			otherCommand(),
			reviewCommand(),
			testCommand(),
			cleanCommand(),
			mergeCommand(),
			reconcileCommand(),
			verifyCommand(),
			fixCommand(),
			statusCommand(),
			listCommand(),
			milestoneCommand(),
			syncCommand(),
			notifyCommand(),
			completionCommand(),
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
		Name:         "edit",
		Usage:        "Create or edit a task in the design directory",
		ArgsUsage:    "<task-name>",
		BashComplete: completeTasks(design.StatePending),
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
		Name:         "run",
		Usage:        "Execute a design task",
		ArgsUsage:    "<task-name>",
		BashComplete: completeTasks(design.StatePending),
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
			&cli.BoolFlag{
				Name:    "no-notify",
				Aliases: []string{"N"},
				Usage:   "Disable desktop notifications when confirmation is needed",
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
			r.Notify = true
			if c.Bool("no-auto-accept") {
				r.AutoAccept = false
			}
			if c.Bool("no-plan") {
				r.PlanMode = false
			}
			if c.Bool("no-notify") {
				r.Notify = false
			}
			if m := c.String("model"); m != "" {
				r.Model = m
			}

			return r.Run(c.Args().Get(0))
		},
	}
}

func groupCommand() *cli.Command {
	return &cli.Command{
		Name:  "group",
		Usage: "Manage and run task groups",
		Description: "List groups, list tasks within a group, run all pending tasks " +
			"in a group, or merge all review/merge tasks in a group.",
		Subcommands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List available groups",
				Action: func(_ *cli.Context) error {
					r, err := newRunner()
					if err != nil {
						return err
					}
					return r.GroupList()
				},
			},
			{
				Name:         "tasks",
				Usage:        "List all tasks in a group (all states)",
				ArgsUsage:    "<group-name>",
				BashComplete: completeAllGroups,
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra group tasks <group-name>")
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					return r.GroupTasks(c.Args().Get(0))
				},
			},
			{
				Name:         "run",
				Usage:        "Run all pending tasks in a group sequentially",
				ArgsUsage:    "<group-name>",
				BashComplete: completeGroups,
				Description: "Runs all pending tasks in the named group in alphabetical order. " +
					"Each task gets its own cloned work directory. Stops on the first error.",
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
					&cli.BoolFlag{
						Name:    "no-notify",
						Aliases: []string{"N"},
						Usage:   "Disable desktop notifications when confirmation is needed",
					},
					&cli.StringFlag{
						Name:  "model",
						Usage: "Override the Claude model",
					},
				},
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra group run <group-name>")
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					r.AutoAccept = true
					r.PlanMode = true
					r.Notify = true
					if c.Bool("no-auto-accept") {
						r.AutoAccept = false
					}
					if c.Bool("no-plan") {
						r.PlanMode = false
					}
					if c.Bool("no-notify") {
						r.Notify = false
					}
					if m := c.String("model"); m != "" {
						r.Model = m
					}
					return r.RunGroup(c.Args().Get(0))
				},
			},
			{
				Name:         "merge",
				Usage:        "Merge all review/merge tasks in a group sequentially",
				ArgsUsage:    "<group-name>",
				BashComplete: completeGroups,
				Description: "Merges all tasks in review or merge state in the named group, " +
					"in alphabetical order. Each task rebases onto the updated main. " +
					"Stops on the first error.",
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
					&cli.BoolFlag{
						Name:    "no-notify",
						Aliases: []string{"N"},
						Usage:   "Disable desktop notifications when confirmation is needed",
					},
					&cli.StringFlag{
						Name:  "model",
						Usage: "Override the Claude model",
					},
				},
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra group merge <group-name>")
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					r.AutoAccept = true
					r.PlanMode = true
					r.Notify = true
					if c.Bool("no-auto-accept") {
						r.AutoAccept = false
					}
					if c.Bool("no-plan") {
						r.PlanMode = false
					}
					if c.Bool("no-notify") {
						r.Notify = false
					}
					if m := c.String("model"); m != "" {
						r.Model = m
					}
					return r.MergeGroup(c.Args().Get(0))
				},
			},
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

// MarshalYAML quotes string values that start with a digit so the chroma YAML
// lexer tokenizes them as strings rather than splitting them into number + text.
func (s statusOutput) MarshalYAML() (any, error) {
	type raw statusOutput
	var n yaml.Node
	if err := n.Encode(raw(s)); err != nil {
		return nil, err
	}
	quoteDigitScalars(&n)
	return &n, nil
}

// quoteDigitScalars walks a yaml.Node tree and applies double-quote style
// to string scalars whose value starts with a digit.
func quoteDigitScalars(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		runes := []rune(n.Value)
		if len(runes) > 0 && unicode.IsDigit(runes[0]) {
			n.Style = yaml.DoubleQuotedStyle
		}
	}
	for _, child := range n.Content {
		quoteDigitScalars(child)
	}
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
				sort.Strings(*ss.dest)
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

			var labels []string
			for _, t := range tasks {
				label := t.Name
				if t.Group != "" {
					label = t.Group + "/" + t.Name
				}
				labels = append(labels, label)
			}
			sort.Strings(labels)
			for _, label := range labels {
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
			r, err := newRunner()
			if err != nil {
				return err
			}
			return r.Sync(c.StringSlice("label"))
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
func stateCommand(name, usage, description, runUsage string, states []design.TaskState, ops stateOps) *cli.Command {
	complete := completeTasks(states...)
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
				Name:         "view",
				Usage:        "Print task content from " + name + " state",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
				Name:         "edit",
				Usage:        "Open a task in " + name + " state in the editor",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
				Name:         "rm",
				Usage:        "Move a task from " + name + " to abandoned",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
				Name:         "run",
				Usage:        runUsage,
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
					&cli.BoolFlag{
						Name:    "no-notify",
						Aliases: []string{"N"},
						Usage:   "Disable desktop notifications when confirmation is needed",
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
					r.Notify = true
					if c.Bool("no-auto-accept") {
						r.AutoAccept = false
					}
					if c.Bool("no-plan") {
						r.PlanMode = false
					}
					if c.Bool("no-notify") {
						r.Notify = false
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
	complete := completeTasks(design.StateReview)
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
				Name:         "view",
				Usage:        "Print task content from review state",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
				Name:         "edit",
				Usage:        "Open a task in review state in the editor",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
				Name:         "rm",
				Usage:        "Move a task from review to abandoned",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
				Name:         "run",
				Usage:        "Run an interactive review session",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
					&cli.BoolFlag{
						Name:    "no-notify",
						Aliases: []string{"N"},
						Usage:   "Disable desktop notifications when confirmation is needed",
					},
					&cli.StringFlag{
						Name:  "model",
						Usage: "Override the Claude model",
					},
					&cli.BoolFlag{
						Name:    "rebase",
						Aliases: []string{"r"},
						Usage:   "Rebase onto origin/main before reviewing",
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
					r.Notify = true
					if c.Bool("no-auto-accept") {
						r.AutoAccept = false
					}
					if c.Bool("no-plan") {
						r.PlanMode = false
					}
					if c.Bool("no-notify") {
						r.Notify = false
					}
					if m := c.String("model"); m != "" {
						r.Model = m
					}
					r.Rebase = c.Bool("rebase")
					return r.Review(c.Args().Get(0))
				},
			},
			{
				Name:         "diff",
				Usage:        "Show git diff for all changes on the task's branch",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: hydra review diff <task-name>")
					}
					r, err := newRunner()
					if err != nil {
						return err
					}
					return r.ReviewDiff(c.Args().Get(0))
				},
			},
			{
				Name:         "dev",
				Usage:        "Run the dev command from hydra.yml in the task's work directory",
				ArgsUsage:    "<task-name>",
				BashComplete: complete,
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
		Name:         "test",
		Usage:        "Add tests for a task in review state",
		ArgsUsage:    "<task-name>",
		BashComplete: completeTasks(design.StateReview),
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
			&cli.BoolFlag{
				Name:    "no-notify",
				Aliases: []string{"N"},
				Usage:   "Disable desktop notifications when confirmation is needed",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Override the Claude model",
			},
			&cli.BoolFlag{
				Name:    "rebase",
				Aliases: []string{"r"},
				Usage:   "Rebase onto origin/main before testing",
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
			r.Notify = true
			if c.Bool("no-auto-accept") {
				r.AutoAccept = false
			}
			if c.Bool("no-plan") {
				r.PlanMode = false
			}
			if c.Bool("no-notify") {
				r.Notify = false
			}
			if m := c.String("model"); m != "" {
				r.Model = m
			}
			r.Rebase = c.Bool("rebase")

			return r.Test(c.Args().Get(0))
		},
	}
}

func cleanCommand() *cli.Command {
	return &cli.Command{
		Name:         "clean",
		Usage:        "Run the clean command from hydra.yml in a task's work directory",
		ArgsUsage:    "<task-name>",
		BashComplete: completeAllTasks,
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
		[]design.TaskState{design.StateReview, design.StateMerge},
		stateOps{
			list: (*runner.Runner).MergeList,
			view: (*runner.Runner).MergeView,
			edit: (*runner.Runner).MergeEdit,
			rm:   (*runner.Runner).MergeRemove,
			run:  (*runner.Runner).Merge,
		},
	)
}

func reconcileCommand() *cli.Command {
	return &cli.Command{
		Name:  "reconcile",
		Usage: "Merge completed tasks into functional.md and clean up",
		Description: "Reads all completed task documents, uses Claude to synthesize " +
			"their requirements into functional.md, then removes the completed task files.",
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
			&cli.BoolFlag{
				Name:    "no-notify",
				Aliases: []string{"N"},
				Usage:   "Disable desktop notifications when confirmation is needed",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Override the Claude model",
			},
		},
		Action: func(c *cli.Context) error {
			r, err := newRunner()
			if err != nil {
				return err
			}

			r.AutoAccept = true
			r.PlanMode = true
			r.Notify = true
			if c.Bool("no-auto-accept") {
				r.AutoAccept = false
			}
			if c.Bool("no-plan") {
				r.PlanMode = false
			}
			if c.Bool("no-notify") {
				r.Notify = false
			}
			if m := c.String("model"); m != "" {
				r.Model = m
			}

			return r.Reconcile()
		},
	}
}

func verifyCommand() *cli.Command {
	return &cli.Command{
		Name:  "verify",
		Usage: "Verify all functional.md requirements against the codebase",
		Description: "Uses Claude to check that every requirement in functional.md " +
			"is implemented and tests pass on the current main branch.",
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
			&cli.BoolFlag{
				Name:    "no-notify",
				Aliases: []string{"N"},
				Usage:   "Disable desktop notifications when confirmation is needed",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Override the Claude model",
			},
		},
		Action: func(c *cli.Context) error {
			r, err := newRunner()
			if err != nil {
				return err
			}

			r.AutoAccept = true
			r.PlanMode = true
			r.Notify = true
			if c.Bool("no-auto-accept") {
				r.AutoAccept = false
			}
			if c.Bool("no-plan") {
				r.PlanMode = false
			}
			if c.Bool("no-notify") {
				r.Notify = false
			}
			if m := c.String("model"); m != "" {
				r.Model = m
			}

			return r.Verify()
		},
	}
}

func fixCommand() *cli.Command {
	return &cli.Command{
		Name:  "fix",
		Usage: "Scan for and fix project issues",
		Description: "Checks for duplicate task names, stale locks, work directories on " +
			"wrong branches, remote URL mismatches, missing state directories, and orphaned " +
			"work directories. Reports all issues found, then prompts for confirmation " +
			"before applying fixes. Use -y to skip confirmation.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "Skip confirmation prompt and apply fixes immediately",
			},
		},
		Action: func(c *cli.Context) error {
			r, err := newRunner()
			if err != nil {
				return err
			}
			return r.Fix(c.Bool("yes"))
		},
	}
}

func notifyCommand() *cli.Command {
	return &cli.Command{
		Name:      "notify",
		Usage:     "Send a desktop notification",
		ArgsUsage: "<message>",
		Description: "Sends a desktop notification with the given message. " +
			"If a notify command is configured in hydra.yml, it is executed with " +
			"the title and message as arguments. Otherwise, uses the platform's " +
			"native notification API (D-Bus on Linux, osascript on macOS).\n\n" +
			"Used by Claude during task runs to alert the user when input is needed.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "title",
				Aliases: []string{"t"},
				Usage:   "Notification title (defaults to 'hydra')",
				Value:   "hydra",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() < 1 {
				return errors.New("usage: hydra notify <message>")
			}
			message := strings.Join(c.Args().Slice(), " ")
			title := c.String("title")

			// Check for custom notify command in hydra.yml.
			cfg, err := config.Discover()
			if err == nil {
				r, rErr := runner.New(cfg)
				if rErr == nil && r.TaskRunner != nil {
					if handled, nErr := r.TaskRunner.RunNotify(title, message); handled {
						return nErr
					}
				}
			}

			return notify.Send(title, message)
		},
	}
}

// setTerminalTitle sets the xterm window title to a compact summary
// including the operation, task name, and PID.
func setTerminalTitle(c *cli.Context) {
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		return
	}
	args := c.Args().Slice()
	if len(args) == 0 || args[0] == "completion" || args[0] == "status" || args[0] == "help" {
		return
	}
	// Skip title when any help flag is present.
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return
		}
	}
	title := fmt.Sprintf("hydra %s [pid:%d]", strings.Join(args, " "), os.Getpid())
	fmt.Fprintf(os.Stderr, "\033]0;%s\007", title)
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
		Usage: "Manage milestones and their promises",
		Description: "Create, edit, list, verify, repair, and deliver milestones. " +
			"Each milestone is a date-based markdown file where ## headings are promises. " +
			"Hydra creates tasks for each promise and tracks their completion.",
		Subcommands: []*cli.Command{
			milestoneCreateCommand(),
			milestoneEditCommand(),
			milestoneListCommand(),
			milestoneVerifyCommand(),
			milestoneRepairCommand(),
			milestoneDeliverCommand(),
		},
	}
}

func milestoneCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "Create a new milestone",
		ArgsUsage: "[date]",
		Description: "Creates a new milestone. If a date argument is given (YYYY-MM-DD), uses it; " +
			"otherwise reads the date from stdin. Opens the editor with a template, then " +
			"creates task files for each promise (## heading).",
		Action: func(c *cli.Context) error {
			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			var dateInput string
			if c.NArg() > 0 {
				dateInput = c.Args().Get(0)
			} else {
				fmt.Print("Milestone date (YYYY-MM-DD): ")
				if _, err := fmt.Scanln(&dateInput); err != nil {
					return errors.New("no date provided")
				}
			}

			date, err := design.NormalizeDate(dateInput)
			if err != nil {
				return err
			}

			editor, err := resolveEditor()
			if err != nil {
				return err
			}

			// Write template to temp file and open editor.
			tmpFile, err := os.CreateTemp("", "hydra-milestone-*.md")
			if err != nil {
				return fmt.Errorf("creating temp file: %w", err)
			}
			tmpPath := tmpFile.Name()
			if _, err := tmpFile.WriteString(design.MilestoneTemplate); err != nil {
				_ = tmpFile.Close()
				_ = os.Remove(tmpPath)
				return fmt.Errorf("writing template: %w", err)
			}
			_ = tmpFile.Close()
			defer func() { _ = os.Remove(tmpPath) }()

			if err := design.RunEditorOnFile(editor, tmpPath, os.Stdin, os.Stdout, os.Stderr); err != nil {
				return err
			}

			content, err := os.ReadFile(tmpPath) //nolint:gosec // path is from our own temp file
			if err != nil {
				return fmt.Errorf("reading temp file: %w", err)
			}

			if len(strings.TrimSpace(string(content))) == 0 {
				return errors.New("empty milestone, aborting")
			}

			dd, err := design.NewDir(cfg.DesignDir)
			if err != nil {
				return err
			}

			m, err := dd.CreateMilestone(date, string(content))
			if err != nil {
				return err
			}

			fmt.Printf("Created milestone %s\n", m.Date)

			// Create initial task files.
			result, err := dd.RepairMilestone(m)
			if err != nil {
				return fmt.Errorf("creating tasks: %w", err)
			}

			for _, slug := range result.Created {
				fmt.Printf("  Created task: %s/%s\n", design.MilestoneTaskGroup(date), slug)
			}

			return nil
		},
	}
}

func milestoneEditCommand() *cli.Command {
	return &cli.Command{
		Name:         "edit",
		Usage:        "Edit an existing milestone",
		ArgsUsage:    "<date>",
		BashComplete: completeMilestones,
		Description:  "Opens the milestone file for the given date in your editor.",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra milestone edit <date>")
			}

			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dd, err := design.NewDir(cfg.DesignDir)
			if err != nil {
				return err
			}

			date := c.Args().Get(0)
			m, err := dd.FindMilestone(date)
			if err != nil {
				return err
			}

			editor, err := resolveEditor()
			if err != nil {
				return err
			}

			return design.RunEditorOnFile(editor, m.FilePath, os.Stdin, os.Stdout, os.Stderr)
		},
	}
}

func milestoneListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List milestones",
		Description: "Lists outstanding (undelivered) milestones, delivered milestones, " +
			"and historical scores. Use --outstanding to show only undelivered milestones.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "outstanding",
				Usage: "Show only undelivered milestones",
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

			milestones, err := dd.Milestones()
			if err != nil {
				return err
			}

			if c.Bool("outstanding") {
				if len(milestones) == 0 {
					fmt.Println("No outstanding milestones.")
					return nil
				}
				for _, m := range milestones {
					fmt.Println(m.Date)
				}
				return nil
			}

			any := false

			if len(milestones) > 0 {
				any = true
				fmt.Println("Outstanding:")
				for _, m := range milestones {
					fmt.Printf("  - %s\n", m.Date)
				}
				fmt.Println()
			}

			delivered, err := dd.DeliveredMilestones()
			if err != nil {
				return err
			}

			if len(delivered) > 0 {
				any = true
				fmt.Println("Delivered:")
				for _, m := range delivered {
					fmt.Printf("  - %s\n", m.Date)
				}
				fmt.Println()
			}

			history, err := dd.MilestoneHistory()
			if err != nil {
				return err
			}

			if len(history) > 0 {
				any = true
				fmt.Println("History:")
				for _, h := range history {
					fmt.Printf("  - %s [%s]\n", h.Date, h.Score)
				}
				fmt.Println()
			}

			if !any {
				fmt.Println("No milestones found.")
			}

			return nil
		},
	}
}

func milestoneVerifyCommand() *cli.Command {
	return &cli.Command{
		Name:  "verify",
		Usage: "Verify outstanding milestones",
		Description: "Checks all undelivered milestones with a date on or before today. " +
			"For each, verifies that all promises have completed tasks. " +
			"Milestones where all promises are kept are automatically marked as delivered.",
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

			today := time.Now().Format("2006-01-02")
			found := false

			for _, m := range milestones {
				if m.Date > today {
					continue
				}
				found = true

				result, err := dd.VerifyMilestone(&m)
				if err != nil {
					return err
				}

				fmt.Printf("Milestone %s:\n", result.Date)

				if result.AllKept {
					fmt.Println("  All promises kept!")
					if err := dd.DeliverMilestone(&m); err != nil {
						return err
					}
					fmt.Println("  (automatically delivered)")
				} else {
					if len(result.Missing) > 0 {
						fmt.Println("  Missing tasks:")
						for _, s := range result.Missing {
							fmt.Printf("    - %s\n", s)
						}
					}
					if len(result.Incomplete) > 0 {
						fmt.Println("  Incomplete tasks:")
						for _, s := range result.Incomplete {
							fmt.Printf("    - %s\n", s)
						}
					}
				}
				fmt.Println()
			}

			if !found {
				fmt.Println("No milestones due for verification.")
			}

			return nil
		},
	}
}

func milestoneRepairCommand() *cli.Command {
	return &cli.Command{
		Name:         "repair",
		Usage:        "Create missing task files for a milestone",
		ArgsUsage:    "<date>",
		BashComplete: completeMilestones,
		Description: "Parses the milestone's promises and creates task files for any " +
			"promise that doesn't have a corresponding task.",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra milestone repair <date>")
			}

			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dd, err := design.NewDir(cfg.DesignDir)
			if err != nil {
				return err
			}

			date := c.Args().Get(0)
			m, err := dd.FindMilestone(date)
			if err != nil {
				return err
			}

			result, err := dd.RepairMilestone(m)
			if err != nil {
				return err
			}

			group := design.MilestoneTaskGroup(date)

			if len(result.Created) > 0 {
				fmt.Println("Created:")
				for _, s := range result.Created {
					fmt.Printf("  - %s/%s\n", group, s)
				}
			}
			if len(result.Skipped) > 0 {
				fmt.Println("Skipped (already exist):")
				for _, s := range result.Skipped {
					fmt.Printf("  - %s/%s\n", group, s)
				}
			}
			if len(result.Created) == 0 && len(result.Skipped) == 0 {
				fmt.Println("No promises found in milestone.")
			}

			return nil
		},
	}
}

func milestoneDeliverCommand() *cli.Command {
	return &cli.Command{
		Name:         "deliver",
		Usage:        "Mark a milestone as delivered",
		ArgsUsage:    "<date>",
		BashComplete: completeMilestones,
		Description:  "Moves a milestone to the delivered directory.",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return errors.New("usage: hydra milestone deliver <date>")
			}

			cfg, err := config.Discover()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dd, err := design.NewDir(cfg.DesignDir)
			if err != nil {
				return err
			}

			date := c.Args().Get(0)
			m, err := dd.FindMilestone(date)
			if err != nil {
				return err
			}

			if err := dd.DeliverMilestone(m); err != nil {
				return err
			}

			fmt.Printf("Delivered milestone %s\n", date)
			return nil
		},
	}
}
