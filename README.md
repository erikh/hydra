# Hydra

![Hydra example](example.png)

AI-driven local pull request workflow where Claude is the only contributor.

## What is Hydra?

You don't need a CI system, VM infrastructure, web interfaces, pull requests. You don't even need to push to do anything. You just need Claude.

Hydra turns markdown documents that describe desired changes into branches, code, and commits without you writing a line. Each task is just a markdown file describing what to build or fix. Hydra assembles context from your design docs, hands the full document to Claude Code — including instructions to run tests, lint, and commit — and pushes a branch ready for your review.

## Concepts

**Task** — A markdown file describing a unit of work. Lives in `tasks/` as a `.md` file, optionally inside a group subdirectory.

**Group** — A subdirectory of `tasks/` containing related tasks. An optional `group.md` provides shared context injected into every task in the group.

**Design Directory** — The directory tree holding tasks, rules, configuration, and state. Registered at `hydra init` time.

**State Machine** — Every task moves through a fixed set of states:

```
                    ┌──────────┐
                    │ pending  │
                    └────┬─────┘
                         │  hydra run
                         v
                    ┌──────────┐
              ┌─────│  review  │─────┐
              │     └────┬─────┘     │
              │          │           │  hydra review rm /
              │          │  hydra    │  hydra merge rm
              │          │  merge    │
              │          │  run      v
              │     ┌────┴─────┐  ┌───────────┐
              │     │  merge   │  │ abandoned  │
              │     └────┬─────┘  └───────────┘
              │          │
              │          │  success
              v          v
         ┌─────────────────┐
         │   completed     │
         └─────────────────┘
```

## Design Directory Structure

```
design-dir/
├── rules.md                          # Rules injected into every task context
├── lint.md                           # Code quality and linting rules
├── functional.md                     # Functional test requirements
├── hydra.yml                         # Configuration (commands, model, API type)
├── tasks/                            # Pending task files
│   ├── {name}.md                     # Individual task
│   ├── {group}/                      # Task group (subdirectory)
│   │   ├── group.md                  # Optional group heading (shared context)
│   │   └── {name}.md                 # Grouped task
│   └── issues/                       # Imported issues (created by hydra sync)
│       ├── group.md                  # Auto-generated group heading
│       └── {number}-{slug}.md        # Issue task files
├── other/                            # Miscellaneous supporting documents
├── state/
│   ├── record.json                   # SHA-to-task mapping for all completed runs
│   ├── review/                       # Tasks finished, awaiting review
│   ├── merge/                        # Tasks reviewed, ready to merge
│   ├── completed/                    # Tasks that completed the full lifecycle
│   └── abandoned/                    # Abandoned tasks
└── milestone/
    ├── {date}.md                     # Milestone target (e.g., 2025-06-01.md)
    ├── delivered/                    # Milestones marked as delivered
    │   └── {date}.md
    └── history/
        └── {date}-{grade}.md         # Historical score (e.g., 2025-06-01-B.md)
```

`rules.md`, `lint.md`, and `functional.md` are optional — empty or missing files are silently omitted from the assembled document.

## Installation

```
go install github.com/erikh/hydra@latest
```

### Credentials

Hydra supports two execution paths, chosen automatically:

1. **Claude Code CLI** (preferred): If the `claude` CLI is installed and on your PATH, hydra shells out to it directly. This uses whatever authentication the CLI has configured (OAuth login via `claude login`, etc.) and provides Claude Code's own interactive terminal UI.
2. **Direct API** (fallback): If the `claude` CLI is not found, hydra calls the Anthropic API directly using `ANTHROPIC_API_KEY` and provides its own built-in TUI.

## Quick Example: Issue to Merge

```sh
# Initialize a project with a source repo and design directory
hydra init https://github.com/you/project.git ./my-design

# Import open issues from GitHub
hydra sync

# Run a task — Claude implements, tests, lints, and commits; branch pushed
hydra run issues/42-fix-bug

# Add tests — Claude reads the task, adds missing tests, runs test/lint
hydra test 42-fix-bug

# Review the implementation — Claude validates commit messages and test coverage
hydra review run 42-fix-bug

# Merge — pre-merge verification, rebase onto main, push, close issue
hydra merge run 42-fix-bug
```

## Command Reference

### `hydra init <source-repo-url> <design-dir>`

Initializes a hydra project. Clones the source repository into `./repo`, registers the design directory, and creates `.hydra/config.json`. If the design directory is empty, scaffolds the full directory structure with placeholder files. A convenience symlink `./design` is created pointing to the design directory.

### `hydra edit <task-name>`

Opens your editor to create or edit a task file. The editor is resolved from `$VISUAL`, then `$EDITOR`. The task name must not contain `/`.

### `hydra run <task-name>`

Executes the full task lifecycle:

1. Finds the pending task by name (supports `group/name` for grouped tasks)
2. Acquires a per-task file lock — only one instance of the same task runs at a time; different tasks run concurrently
3. Clones the source repo into a per-task work directory (`work/{task-name}/`)
4. Creates a git branch `hydra/<task-name>`
5. Assembles a document from `rules.md`, `lint.md`, the task content, `functional.md`, and commit instructions
6. Runs the `before` command if configured in `hydra.yml`
7. Opens a Claude session — Claude implements the changes, runs tests/lint, and commits with a descriptive message (GPG-signed if a signing key is configured)
8. Verifies Claude committed (HEAD moved), records the SHA, pushes, and moves the task to review

**Flags:**

- `--no-auto-accept` / `-Y` — Disable auto-accept (prompt for each tool call)
- `--no-plan` / `-P` — Disable plan mode (skip plan approval, run fully autonomously)
- `--model` — Override the Claude model (e.g. `--model claude-haiku-4-5-20251001`)

By default, hydra auto-accepts all tool calls and starts Claude in plan mode.

### `hydra group`

Manage and run task groups.

```sh
hydra group list                   # List available groups
hydra group tasks <group-name>     # List all tasks in a group (all states)
hydra group run <group-name>       # Run all pending tasks in a group sequentially
hydra group merge <group-name>     # Merge all review/merge tasks in a group sequentially
```

`hydra group list` discovers and prints unique group names from pending tasks.

`hydra group tasks` shows all tasks in the named group across all states, with state labels.

`hydra group run` executes all pending tasks in the named group in alphabetical order. Each task gets its own cloned work directory. Stops on the first error.

`hydra group merge` merges all tasks in review or merge state in the named group, in alphabetical order. Each task rebases onto the updated main. Stops on the first error.

**`run` and `merge` flags:** `--no-auto-accept` / `-Y`, `--no-plan` / `-P`, `--model`

### `hydra review`

Manage and run interactive review sessions on tasks that have been run.

```sh
hydra review list                  # List tasks in review state
hydra review view <task-name>      # Print task content
hydra review edit <task-name>      # Open task in editor
hydra review rm <task-name>        # Move task to abandoned
hydra review run <task-name>       # Run interactive review session
hydra review dev <task-name>       # Run the dev command in the task's work directory
```

`hydra review run` runs the `before` command if configured, then opens a Claude session where Claude reviews the implementation and validates:

- **Commit messages** — reads the git log and verifies commit messages accurately describe the changes per the task document; amends if needed
- **Test coverage** — identifies every feature described in the task document and verifies each has test coverage; adds missing tests

If Claude commits changes, they are pushed automatically. The task stays in review state after the session.

`hydra review dev` runs the `dev` command from `hydra.yml` in the task's work directory. The process runs until it exits or is terminated with Ctrl+C (SIGINT), SIGTERM, or SIGHUP. Use this to start a local dev server, file watcher, or hot-reload process while reviewing a task.

**`run` flags:** `--no-auto-accept` / `-Y`, `--no-plan` / `-P`, `--model`

### `hydra test <task-name>`

Runs the `before` command if configured, then opens a test-focused Claude session on a task in review state. Claude reads the task description and existing implementation, then:

1. Identifies every feature, behavior, and edge case described in the task document
2. Checks which features already have test coverage
3. Adds tests for any features or behaviors that lack coverage
4. Runs test and lint commands from `hydra.yml` and fixes any issues

If Claude commits changes, they are pushed automatically. The task stays in review state.

**Flags:** `--no-auto-accept` / `-Y`, `--no-plan` / `-P`, `--model`

### `hydra clean <task-name>`

Runs the `clean` command from `hydra.yml` in the task's work directory, regardless of which state the task is in. Use this to reset build artifacts, remove generated files, or restore the work directory to a clean state.

The task can be in any state (pending, review, merge, completed, or abandoned). The `clean` command must be configured in `hydra.yml`.

### `hydra merge`

Manage and run the merge workflow for reviewed tasks.

```sh
hydra merge list                   # List tasks in merge state
hydra merge view <task-name>       # Print task content
hydra merge edit <task-name>       # Open task in editor
hydra merge rm <task-name>         # Move task to abandoned
hydra merge run <task-name>        # Run merge workflow
```

`hydra merge run` performs:

1. Attempts to rebase onto `origin/main`; if conflicts occur, the rebase is aborted and the conflict file list is recorded
2. Runs the `before` command if configured in `hydra.yml`
3. Opens a single Claude session with a comprehensive document covering: conflict resolution (if needed), commit message validation, test coverage verification, and test/lint commands
4. Force-pushes the branch, rebases into main, pushes, records the SHA, moves the task to completed, closes the remote issue if applicable, and deletes the remote feature branch

**`run` flags:** `--no-auto-accept` / `-Y`, `--no-plan` / `-P`, `--model`

### `hydra reconcile`

Reads all completed task documents, uses Claude to synthesize their requirements into `functional.md`, then removes the completed task files. This keeps `functional.md` as the project's living specification — a concise description of what the software does, organized by feature area rather than by task.

The workflow:

1. Collects all completed tasks from `state/completed/`
2. Clones/syncs the source repo into `work/_reconcile/`
3. Copies `functional.md` into the work directory for Claude to edit
4. Opens a Claude session with the current functional spec, all completed task contents, and instructions to merge them
5. Claude reads the codebase to understand what was actually implemented, then updates `functional.md`
6. The updated `functional.md` is copied back to the design directory
7. Completed task files are deleted

If no completed tasks exist, the command exits with an error. If Claude fails, no tasks are deleted and `functional.md` is not modified.

**Flags:** `--no-auto-accept` / `-Y`, `--no-plan` / `-P`, `--model`

### `hydra verify`

Uses Claude to verify that every requirement in `functional.md` is satisfied by the current codebase. This is a read-only check — no source code is modified.

The workflow:

1. Reads `functional.md` (errors if empty)
2. Clones/syncs the source repo into `work/_verify/`
3. Opens a Claude session where Claude reads the code and runs tests
4. Claude creates `verify-passed.txt` if all requirements are met, or `verify-failed.txt` listing failures

If verification passes, prints a success message and automatically runs a sync (importing open issues and cleaning up completed tasks). If sync fails, a warning is printed but the verify command still succeeds. If verification fails, prints the failure details and exits with an error.

**Flags:** `--no-auto-accept` / `-Y`, `--no-plan` / `-P`, `--model`

### `hydra other`

Manage miscellaneous files in the `other/` directory.

```sh
hydra other list           # List files in other/
hydra other add <name>     # Create a new file via editor
hydra other view <name>    # Print file content
hydra other edit <name>    # Edit an existing file
hydra other rm <name>      # Remove a file
```

### `hydra sync`

Imports open issues from GitHub or Gitea as task files under `tasks/issues/`. Existing issues (matched by number) are skipped. The API type is auto-detected from the source repo URL or can be set via `api_type` in `hydra.yml`.

After importing, sync cleans up completed and abandoned tasks: remote feature branches are deleted and the corresponding issues are closed with a comment that includes the merge commit SHA.

**Flags:** `--label` — Filter issues by label (repeatable)

**Auth:** Set `GITHUB_TOKEN` (GitHub) or `GITEA_TOKEN` (Gitea) for private repos.

### `hydra list`

Lists all pending tasks. Grouped tasks are displayed as `group/name`.

### `hydra status`

Shows tasks grouped by state (pending, review, merge, completed, abandoned) and any currently running task.

**Flags:**

- `--json` / `-j` — Output as JSON instead of YAML
- `--no-color` — Disable syntax highlighting

When stdout is a TTY, output is syntax-highlighted using the active color theme.

### `hydra milestone`

Manage milestones and their promises. Each milestone is a date-based markdown file where `##` headings are promises. Hydra creates tasks for each promise and tracks their completion.

```sh
hydra milestone create [date]         # Create a new milestone (opens editor)
hydra milestone edit <date>           # Edit an existing milestone
hydra milestone list                  # List outstanding, delivered, and historical milestones
hydra milestone list --outstanding    # List only undelivered milestones
hydra milestone verify                # Verify due milestones (auto-delivers if all kept)
hydra milestone repair <date>         # Create missing task files for promises
hydra milestone deliver <date>        # Mark a milestone as delivered
```

`hydra milestone create` normalizes the date, opens your editor with a template, then creates task files under `tasks/milestone-{date}/` for each `##` heading.

`hydra milestone verify` checks all undelivered milestones with a date on or before today. For each promise, it checks whether the corresponding task has reached the completed state. Milestones where all promises are kept are automatically marked as delivered.

`hydra milestone repair` re-scans the milestone file and creates task files for any promises that don't have one yet. Existing tasks are left untouched.

### `hydra completion`

Print or manage shell tab completion.

```sh
hydra completion bash        # Print bash completion script to stdout
hydra completion zsh         # Print zsh completion script to stdout
hydra completion install     # Inject completion into your shell RC file
hydra completion uninstall   # Remove completion from your shell RC file
```

`install` injects a one-liner into your RC file (`~/.bashrc` or `~/.zshrc`) that evals `hydra completion <shell>` at startup, guarded by a `command -v hydra` check so it no-ops if hydra is not installed. Supports bash and zsh, detected from `$SHELL`. On first run, hydra automatically prompts to install completion; the decision is saved in `~/.hydra/completion`.

## hydra.yml

If `hydra.yml` does not exist in the design directory, it is automatically created with commented-out placeholder commands. This happens during `hydra init` and whenever a runner command is executed.

```yaml
# Model to use for Claude API calls (default: claude-opus-4-6)
model: claude-opus-4-6

# Issue sync API type: "github" or "gitea" (auto-detected from URL if omitted)
api_type: github

# Gitea instance URL (only needed for Gitea when URL can't be parsed)
gitea_url: https://gitea.example.com

# Commands that Claude runs before committing.
#
# IMPORTANT: These commands may run concurrently across multiple hydra tasks,
# each in its own work directory (cloned repo). Make sure your test and lint
# commands are safe to run in parallel without trampling each other. Avoid
# commands that write to shared global state, fixed file paths outside the
# work directory, or shared network ports. Each invocation should be fully
# isolated to its own working tree.
commands:
  before: "make deps"
  clean: "make clean"
  dev: "npm run dev"
  test: "go test ./... -count=1"
  lint: "golangci-lint run ./..."
```

**Command keys:**

- **`before`** — Run by hydra before every Claude invocation (`run`, `review run`, `test`, `merge run`), after the git repository is cloned/prepared. Use this for dependency installation, code generation, or any setup that must happen before Claude starts working. If this command fails, the hydra command aborts.
- **`clean`** — Run by `hydra clean`. Resets build artifacts or restores the work directory. Not run by Claude.
- **`dev`** — Run by `hydra review dev`. Starts a long-lived process (dev server, file watcher, etc.) in the task's work directory. Not run by Claude.
- **`test`** — Run by Claude before committing. Executes the project's test suite.
- **`lint`** — Run by Claude before committing. Executes the project's linter.

**Shell execution:** All commands are executed via `$SHELL -c "<command>"` with the task's work directory as the current working directory. This means shell features like pipes, variable expansion, and subshells work in command strings. If `$SHELL` is not set, `/bin/sh` is used as a fallback.

**Makefile fallback:** If a command key is not configured in `hydra.yml`, hydra checks for a `Makefile` in the task's work directory. If a matching make target exists (e.g. `before:`, `clean:`, `test:`, `lint:`, `dev:`), hydra runs `make <name>` as a fallback. This means projects with a standard Makefile work out of the box without any `hydra.yml` configuration.

**Concurrency safety:** Hydra runs each task in its own cloned work directory under `work/`. Multiple tasks can run concurrently, so your test and lint commands must be safe to execute in parallel. Avoid hardcoded ports, shared temp directories, global lock files, or anything else that would collide when two instances run at the same time. Each command should operate entirely within the current working tree.

## How Claude Commits

Hydra appends commit instructions to every document sent to Claude. These instructions tell Claude to:

1. Run the `test` command if configured in `hydra.yml`
2. Run the `lint` command if configured in `hydra.yml`
3. Stage all changes with `git add -A`
4. Commit with a descriptive message (GPG-signed if a signing key is available)

Claude is explicitly instructed to only use the exact test and lint commands from `hydra.yml` — it must not run individual test files, test functions, or lint checks outside of these commands.

After Claude returns, hydra verifies that a commit was made (HEAD moved). If Claude didn't commit, the run fails with an error. This approach lets Claude write meaningful commit messages that describe the actual changes rather than using a generic task name.

## Interactive TUI

When `hydra run`, `hydra review run`, or `hydra merge run` starts a Claude session, it opens a full-screen terminal UI with streaming output and tool approval.

### Keybindings

| Key | Action |
|-----|--------|
| Ctrl+C | Cancel and quit |
| a | Toggle auto-accept mode |
| Enter / y | Approve tool call |
| Esc / n | Reject tool call |
| Up / Down | Scroll viewport |
| Left / Right | Navigate Accept/Reject buttons |

## Work Directory Structure

Each task gets its own cloned repository under `work/`:

```
project/
├── work/
│   ├── add-feature/              # Ungrouped task work directory
│   ├── backend/
│   │   ├── add-api/              # Grouped task work directory
│   │   └── add-db/               # Another grouped task
│   └── issues/
│       └── 42-fix-bug/           # Issue task work directory
```

Work directories persist between runs. On subsequent runs, hydra syncs the existing directory (fetch) instead of re-cloning.

## Global Configuration (`~/.hydra.yml`)

A global config file at `~/.hydra.yml` lets you customize the TUI color scheme. Colors defined here override pywal and the built-in defaults.

```yaml
colors:
  bg: "#1a1b26"
  fg: "#c0caf5"
  accent: "#7aa2f7"
  success: "#9ece6a"
  error: "#f7768e"
  warning: "#e0af68"
  muted: "#565f89"
  highlight: "#bb9af7"
```

All fields are optional. Missing fields fall through to pywal (`~/.cache/wal/colors.json`) if available, then to the built-in defaults.

**Color priority** (highest to lowest):

1. `~/.hydra.yml`
2. pywal
3. Built-in defaults

## Shell Completion

Hydra supports tab completion for task names. All commands that accept a task name complete with the appropriate tasks for their state (e.g. `hydra run` completes pending tasks, `hydra review run` completes review tasks).

On first run, hydra prompts to inject completion into your shell RC file (`~/.bashrc` or `~/.zshrc`, based on `$SHELL`). The decision is recorded in `~/.hydra/completion` so you're only asked once. The injected line evals `hydra completion <shell>` at startup, guarded by `command -v hydra` so it no-ops if hydra is uninstalled.

You can also manage completion manually:

```sh
hydra completion bash        # Print bash completion script to stdout
hydra completion zsh         # Print zsh completion script to stdout
hydra completion install     # Inject eval into your shell RC file
hydra completion uninstall   # Remove it from your shell RC file
```

Both bash and zsh are supported. The shell type is detected from `$SHELL`.

## Building & Releasing

```sh
# Build snapshot binaries locally
make snapshot

# Tag, push, and release via goreleaser
make full-release

# Override the version (default: vYYYY.MM.DD)
make full-release VERSION=v2026.02.26.1
```

## License

MIT
