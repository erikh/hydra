# Hydra

AI-driven local pull request workflow where Claude is the only contributor.

## What is Hydra?

Hydra turns markdown documents that describe desired changes into branches, code, and commits without you writing a line. Each task is just a markdown file describing what to build or fix. Hydra assembles context from your design docs, drives Claude Code to implement each task, runs tests and linting, and pushes a branch ready for your review.

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

# Run a task — Claude implements it, tests pass, branch pushed
hydra run issues/42-fix-bug

# Review the implementation — Claude checks its own work
hydra review run 42-fix-bug

# Merge — rebase onto main, run tests, fast-forward merge, push, close issue
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
5. Assembles a document from `rules.md`, `lint.md`, the task content, and `functional.md`
6. Opens an interactive TUI session with the Anthropic API
7. Runs `test` and `lint` commands from `hydra.yml`
8. Commits, pushes, records the SHA, and moves the task to review

**Flags:**

- `--auto-accept` / `-y` — Auto-accept all tool calls without prompting
- `--model` — Override the Claude model (e.g. `--model claude-haiku-4-5-20251001`)

### `hydra run-group <group-name>`

Executes all pending tasks in the named group sequentially, in alphabetical order. Each task gets its own cloned work directory. Stops on the first error.

**Flags:**

- `--auto-accept` / `-y` — Auto-accept all tool calls without prompting
- `--model` — Override the Claude model

### `hydra review`

Manage and run interactive review sessions on tasks that have been run.

```sh
hydra review list                  # List tasks in review state
hydra review view <task-name>      # Print task content
hydra review edit <task-name>      # Open task in editor
hydra review rm <task-name>        # Move task to abandoned
hydra review run <task-name> [-y]  # Run interactive review session
```

`hydra review run` opens a TUI session where Claude reviews the implementation, runs tests, and makes corrections. The task stays in review state after the session.

**`run` flags:** `--auto-accept` / `-y`, `--model`

### `hydra merge`

Manage and run the merge workflow for reviewed tasks.

```sh
hydra merge list                   # List tasks in merge state
hydra merge view <task-name>       # Print task content
hydra merge edit <task-name>       # Open task in editor
hydra merge rm <task-name>         # Move task to abandoned
hydra merge run <task-name> [-y]   # Run merge workflow
```

`hydra merge run` performs: rebase onto `origin/main`, resolve conflicts via Claude if needed, run tests/lint, fast-forward merge, push, record SHA, move to completed, and close the remote issue if applicable.

**`run` flags:** `--auto-accept` / `-y`, `--model`

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

**Flags:** `--label` — Filter issues by label (repeatable)

**Auth:** Set `GITHUB_TOKEN` (GitHub) or `GITEA_TOKEN` (Gitea) for private repos.

### `hydra list`

Lists all pending tasks. Grouped tasks are displayed as `group/name`.

### `hydra status`

Shows tasks grouped by state (pending, review, merge, completed, abandoned) and any currently running task.

### `hydra milestone`

Lists milestone targets and historical scores with letter grades (A-F).

## hydra.yml

```yaml
# Model to use for Claude API calls (default: claude-opus-4-6)
model: claude-opus-4-6

# Issue sync API type: "github" or "gitea" (auto-detected from URL if omitted)
api_type: github

# Gitea instance URL (only needed for Gitea when URL can't be parsed)
gitea_url: https://gitea.example.com

# Commands to run after Claude makes changes
commands:
  test: "go test ./... -count=1"
  lint: "golangci-lint run ./..."
```

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
