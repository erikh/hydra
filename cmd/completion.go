package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
)

const completionBeginMarker = "# >>> hydra completion >>>"
const completionEndMarker = "# <<< hydra completion <<<"

const bashCompletionScript = `#!/bin/bash

: ${PROG:=$(basename ${BASH_SOURCE})}

_cli_init_completion() {
  COMPREPLY=()
  _get_comp_words_by_ref "$@" cur prev words cword
}

_cli_bash_autocomplete() {
  if [[ "${COMP_WORDS[0]}" != "source" ]]; then
    local cur opts base words
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    if declare -F _init_completion >/dev/null 2>&1; then
      _init_completion -n "=:" || return
    else
      _cli_init_completion -n "=:" || return
    fi
    words=("${words[@]:0:$cword}")
    if [[ "$cur" == "-"* ]]; then
      requestComp="${words[*]} ${cur} --generate-bash-completion"
    else
      requestComp="${words[*]} --generate-bash-completion"
    fi
    opts=$(eval "${requestComp}" 2>/dev/null)
    COMPREPLY=($(compgen -W "${opts}" -- ${cur}))
    return 0
  fi
}

complete -o bashdefault -o default -o nospace -F _cli_bash_autocomplete $PROG
unset PROG
`

const zshCompletionScript = `#compdef hydra

_cli_zsh_autocomplete() {
  local -a opts
  local cur
  cur=${words[-1]}
  if [[ "$cur" == "-"* ]]; then
    opts=("${(@f)$(${words[@]:0:#words[@]-1} ${cur} --generate-bash-completion)}")
  else
    opts=("${(@f)$(${words[@]:0:#words[@]-1} --generate-bash-completion)}")
  fi

  if [[ "${opts[1]}" != "" ]]; then
    _describe 'values' opts
  else
    _files
  fi
}

compdef _cli_zsh_autocomplete hydra
`

// completionStubPath returns the path to ~/.hydra/completion.
func completionStubPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hydra", "completion")
}

// completionDecided returns true if the completion stub file exists.
func completionDecided() bool {
	p := completionStubPath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// writeCompletionStub writes the decision to the stub file.
func writeCompletionStub(installed bool) error {
	p := completionStubPath()
	if p == "" {
		return errors.New("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	content := "declined\n"
	if installed {
		content = "installed\n"
	}
	return os.WriteFile(p, []byte(content), 0o644)
}

// detectShellType returns "bash" or "zsh" based on $SHELL, or empty if unsupported.
func detectShellType() string {
	shell := os.Getenv("SHELL")
	base := filepath.Base(shell)
	switch base {
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	default:
		return ""
	}
}

// shellRCPath returns ~/.bashrc or ~/.zshrc based on $SHELL.
func shellRCPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch detectShellType() {
	case "bash":
		return filepath.Join(home, ".bashrc")
	case "zsh":
		return filepath.Join(home, ".zshrc")
	default:
		return ""
	}
}

// rcContainsCompletion checks if the RC file already has the hydra completion block.
func rcContainsCompletion(rcPath string) bool {
	data, err := os.ReadFile(rcPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), completionBeginMarker)
}

// injectCompletion appends the completion block to the RC file.
// The injected snippet evals `hydra completion <shell>` at shell startup,
// guarded by a command-existence check so it no-ops if hydra is not installed.
func injectCompletion(rcPath, shell string) error {
	if rcContainsCompletion(rcPath) {
		return nil
	}

	if shell != "bash" && shell != "zsh" {
		return fmt.Errorf("unsupported shell: %s", shell)
	}

	block := fmt.Sprintf("\n%s\ncommand -v hydra &>/dev/null && eval \"$(hydra completion %s)\"\n%s\n",
		completionBeginMarker,
		shell,
		completionEndMarker,
	)

	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(block)
	return err
}

// removeCompletion removes the hydra completion block from the RC file.
func removeCompletion(rcPath string) error {
	data, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	content := string(data)
	if !strings.Contains(content, completionBeginMarker) {
		return nil
	}

	beginIdx := strings.Index(content, completionBeginMarker)
	endIdx := strings.Index(content, completionEndMarker)
	if beginIdx < 0 || endIdx < 0 || endIdx < beginIdx {
		return nil
	}

	// Remove from the newline before the begin marker to the end of the end marker line.
	start := beginIdx
	if start > 0 && content[start-1] == '\n' {
		start--
	}
	end := endIdx + len(completionEndMarker)
	if end < len(content) && content[end] == '\n' {
		end++
	}

	newContent := content[:start] + content[end:]
	return os.WriteFile(rcPath, []byte(newContent), 0o644)
}

// promptCompletionInstall asks the user if they want completion injected.
// It is called from the app's Before hook on first run.
func promptCompletionInstall() {
	if completionDecided() {
		return
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return
	}

	shell := detectShellType()
	if shell == "" {
		return
	}

	rcPath := shellRCPath()
	if rcPath == "" {
		return
	}

	if rcContainsCompletion(rcPath) {
		_ = writeCompletionStub(true)
		return
	}

	fmt.Fprintf(os.Stderr, "Would you like to enable tab completion for hydra in %s? [y/N] ", rcPath)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		_ = writeCompletionStub(false)
		return
	}

	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer == "y" || answer == "yes" {
		if err := injectCompletion(rcPath, shell); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not inject completion: %v\n", err)
			_ = writeCompletionStub(false)
			return
		}
		_ = writeCompletionStub(true)
		fmt.Fprintf(os.Stderr, "Completion installed in %s.\n", rcPath)
	} else {
		_ = writeCompletionStub(false)
		fmt.Fprintf(os.Stderr, "Skipped. You can install later with: hydra completion install\n")
	}
}

// completionCommand returns the `hydra completion` CLI command.
func completionCommand() *cli.Command {
	return &cli.Command{
		Name:  "completion",
		Usage: "Manage shell tab completion",
		Description: "Print or install shell tab completion for hydra. " +
			"Use `hydra completion bash` or `hydra completion zsh` to print the " +
			"completion script to stdout. Use install/uninstall to manage the " +
			"shell RC file injection.",
		Subcommands: []*cli.Command{
			{
				Name:  "bash",
				Usage: "Print bash completion script to stdout",
				Action: func(_ *cli.Context) error {
					fmt.Print(bashCompletionScript)
					return nil
				},
			},
			{
				Name:  "zsh",
				Usage: "Print zsh completion script to stdout",
				Action: func(_ *cli.Context) error {
					fmt.Print(zshCompletionScript)
					return nil
				},
			},
			{
				Name:  "install",
				Usage: "Inject completion into your shell RC file",
				Action: func(_ *cli.Context) error {
					shell := detectShellType()
					if shell == "" {
						return errors.New("unsupported shell (only bash and zsh are supported)")
					}
					rcPath := shellRCPath()
					if rcPath == "" {
						return errors.New("cannot determine RC file path")
					}
					if rcContainsCompletion(rcPath) {
						fmt.Fprintf(os.Stderr, "Completion already installed in %s\n", rcPath)
						_ = writeCompletionStub(true)
						return nil
					}
					if err := injectCompletion(rcPath, shell); err != nil {
						return fmt.Errorf("injecting completion: %w", err)
					}
					_ = writeCompletionStub(true)
					fmt.Fprintf(os.Stderr, "Completion installed in %s.\n", rcPath)
					return nil
				},
			},
			{
				Name:  "uninstall",
				Usage: "Remove completion from your shell RC file",
				Action: func(_ *cli.Context) error {
					shell := detectShellType()
					if shell == "" {
						return errors.New("unsupported shell (only bash and zsh are supported)")
					}
					rcPath := shellRCPath()
					if rcPath == "" {
						return errors.New("cannot determine RC file path")
					}
					if !rcContainsCompletion(rcPath) {
						fmt.Fprintf(os.Stderr, "No hydra completion block found in %s\n", rcPath)
						return nil
					}
					if err := removeCompletion(rcPath); err != nil {
						return fmt.Errorf("removing completion: %w", err)
					}
					// Remove the stub so the user gets prompted again if they want.
					p := completionStubPath()
					if p != "" {
						_ = os.Remove(p)
					}
					fmt.Fprintf(os.Stderr, "Completion removed from %s.\n", rcPath)
					return nil
				},
			},
		},
	}
}

// completeTasks prints task names for the given states, for shell tab completion.
func completeTasks(states ...design.TaskState) func(*cli.Context) {
	return func(cCtx *cli.Context) {
		if cCtx.NArg() > 0 {
			return
		}

		cfg, err := config.Discover()
		if err != nil {
			return
		}

		dd, err := design.NewDir(cfg.DesignDir)
		if err != nil {
			return
		}

		for _, state := range states {
			tasks, err := dd.TasksByState(state)
			if err != nil {
				continue
			}
			for _, t := range tasks {
				label := t.Name
				if t.Group != "" {
					label = t.Group + "/" + t.Name
				}
				fmt.Println(label)
			}
		}
	}
}

// completeGroups prints group names for shell tab completion.
func completeGroups(cCtx *cli.Context) {
	if cCtx.NArg() > 0 {
		return
	}

	cfg, err := config.Discover()
	if err != nil {
		return
	}

	dd, err := design.NewDir(cfg.DesignDir)
	if err != nil {
		return
	}

	tasks, err := dd.PendingTasks()
	if err != nil {
		return
	}

	seen := make(map[string]bool)
	for _, t := range tasks {
		if t.Group != "" && !seen[t.Group] {
			seen[t.Group] = true
			fmt.Println(t.Group)
		}
	}
}

// completeMilestones prints unacknowledged milestone dates for shell tab completion.
func completeMilestones(cCtx *cli.Context) {
	if cCtx.NArg() > 0 {
		return
	}

	cfg, err := config.Discover()
	if err != nil {
		return
	}

	dd, err := design.NewDir(cfg.DesignDir)
	if err != nil {
		return
	}

	milestones, err := dd.Milestones()
	if err != nil {
		return
	}

	for _, m := range milestones {
		fmt.Println(m.Date)
	}
}

// completeAllTasks prints task names across all states.
func completeAllTasks(cCtx *cli.Context) {
	if cCtx.NArg() > 0 {
		return
	}

	cfg, err := config.Discover()
	if err != nil {
		return
	}

	dd, err := design.NewDir(cfg.DesignDir)
	if err != nil {
		return
	}

	tasks, err := dd.AllTasks()
	if err != nil {
		return
	}

	for _, t := range tasks {
		label := t.Name
		if t.Group != "" {
			label = t.Group + "/" + t.Name
		}
		fmt.Println(label)
	}
}
