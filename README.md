# Hydra

Local pull request workflow where Claude is the only contributor.

Hydra turns a tree of markdown design documents into branches, code, and commits — without you writing a line. It assembles context from your design docs, calls the Anthropic API directly with an interactive TUI, runs tests and linting, and pushes a branch ready for your review.

## Installation

```
go install github.com/erikh/hydra@latest
```

### Credentials

Hydra calls the Anthropic API directly. Credentials are resolved in order:

1. **Claude CLI credentials** (checked first): Hydra reads `~/.claude/.credentials.json` (the OAuth token from the Claude CLI)
2. **Environment variable** (fallback): set `ANTHROPIC_API_KEY`

## Quick Start

```sh
# Initialize a project with a source repo and design directory
hydra init https://github.com/you/project.git ./my-design

# Create a new task
hydra edit add-auth

# Edit an existing task
hydra edit add-auth

# Run the task (opens interactive TUI)
hydra run add-auth

# Run with auto-accept (no approval prompts)
hydra run -y add-auth

# Run all tasks in a group
hydra run-group backend

# Review a completed task
hydra review run add-auth

# Merge a reviewed task to main
hydra merge run add-auth

# Import issues from GitHub/Gitea
hydra sync

# Manage other/ files
hydra other list
hydra other add notes.md

# Check progress
hydra status
```

All commands work from subdirectories — hydra searches upward for `.hydra/config.json`.

## Commands

### `hydra init <source-repo-url> <design-dir>`

Initializes a hydra project. Clones the source repository into `./repo`, registers the design directory, and creates `.hydra/config.json`. If the design directory is empty, scaffolds the full directory structure with placeholder files.

A convenience symlink `./design` is created pointing to the design directory.

### `hydra edit <task-name>`

Opens your editor to create or edit a task file. If the task already exists in `tasks/`, it opens the file in-place. Otherwise, it creates a new task via a temp file (only saving if the editor exits successfully with non-empty content). The editor is resolved from `$VISUAL`, then `$EDITOR`. The task name must not contain `/`.

### `hydra run <task-name>`

Executes the full task lifecycle:

1. Finds the pending task by name (supports `group/name` for grouped tasks)
2. Acquires a file lock (`.hydra/hydra.lock`) — only one task runs at a time
3. Clones the source repo into a per-task work directory (`work/{task-name}/`)
4. Creates a git branch `hydra/<task-name>` (or `hydra/<group>/<task-name>`)
5. Assembles a document from `rules.md`, `lint.md`, the task content, and `functional.md`
6. Opens an interactive TUI session with the Anthropic API
7. Claude works through the task using tools (read, write, edit, bash, list, search)
8. Tool calls that modify files or run commands require approval (unless `--auto-accept` / `-y` is set)
9. Verifies Claude produced changes
10. Runs the `test` command from `hydra.yml` (if configured)
11. Runs the `lint` command from `hydra.yml` (if configured)
12. Stages all changes, commits (message: `hydra: <task-name>`), and pushes
13. Records the commit SHA in `state/record.json`
14. Moves the task file to `state/review/`
15. Releases the lock

Each task gets its own cloned repository in `work/`. If a work directory already exists from a previous run, hydra syncs it (fetch + reset) instead of re-cloning. If sync fails, it re-clones fresh.

Commits are GPG-signed if `user.signingkey` is configured in git. Stale locks from dead processes are automatically recovered.

**Flags:**

- `--auto-accept` / `-y`: Auto-accept all tool calls without prompting

### `hydra run-group <group-name>`

Executes all pending tasks in the named group sequentially, in alphabetical order by task name. Each task gets its own cloned work directory, so there's no branch conflict between tasks. Stops immediately on the first error.

If a `group.md` file exists in the group directory (`tasks/{group}/group.md`), its content is included as a `# Group` section in every task's assembled document.

**Flags:**

- `--auto-accept` / `-y`: Auto-accept all tool calls without prompting

### `hydra other`

Manage miscellaneous files in the `other/` directory. These are supporting documents that aren't tasks.

```sh
hydra other list           # List files in other/
hydra other add <name>     # Create a new file via editor
hydra other view <name>    # Print file content
hydra other edit <name>    # Edit an existing file
hydra other rm <name>      # Remove a file
```

File names must not contain `/` or `..` (path traversal prevention).

### `hydra review`

Manage and run interactive review sessions on tasks that have been run.

```sh
hydra review list                  # List tasks in review state
hydra review view <task-name>      # Print task content
hydra review edit <task-name>      # Open task in editor
hydra review rm <task-name>        # Move task to abandoned
hydra review run <task-name> [-y]  # Run interactive review session
```

`hydra review run` opens a TUI session where Claude reviews the implementation, runs tests, and makes corrections. The task stays in review state after the session. If Claude makes changes, they are committed and pushed.

### `hydra merge`

Manage and run the merge workflow for reviewed tasks.

```sh
hydra merge list                   # List tasks in merge state
hydra merge view <task-name>       # Print task content
hydra merge edit <task-name>       # Open task in editor
hydra merge rm <task-name>         # Move task to abandoned
hydra merge run <task-name> [-y]   # Run merge workflow
```

`hydra merge run` performs:

1. Moves the task from review to merge state
2. Fetches and rebases the task branch onto `origin/main`
3. If conflicts occur, opens a TUI session for Claude to resolve them
4. Runs tests and lint after rebase
5. Fast-forward merges into main and pushes
6. Records the SHA and moves the task to completed
7. Closes the remote issue if the task was imported from GitHub/Gitea

If the merge fails, the task stays in merge state so you can retry with `hydra merge run` again.

### `hydra sync`

Imports open issues from GitHub or Gitea as task files under `tasks/issues/`.

- Issues are created as `tasks/issues/{number}-{slugified-title}.md`
- A `group.md` is created automatically for the issues group
- Existing issues (matched by number prefix) are skipped
- The API type is auto-detected from the source repo URL (GitHub if the host is `github.com`, Gitea otherwise), or can be set explicitly via `api_type` in `hydra.yml`

When issue tasks are merged via `hydra merge run`, the corresponding remote issue is automatically closed with a comment linking the commit SHA.

**Flags:**

- `--label`: Filter issues by label (can be specified multiple times)

**Authentication:**

- **GitHub**: Set `GITHUB_TOKEN` for private repos or higher rate limits
- **Gitea**: Set `GITEA_TOKEN` environment variable

### `hydra list`

Lists all pending tasks from the `tasks/` directory. Grouped tasks are displayed as `group/name`.

### `hydra status`

Shows tasks grouped by state (pending, review, merge, completed, abandoned) and displays any currently running task with its PID.

### `hydra milestone`

Lists milestone targets from the `milestone/` directory and historical milestone scores from `milestone/history/`. History entries include a letter grade (A-F) parsed from the filename.

## Interactive TUI

When `hydra run`, `hydra review run`, or `hydra merge run` starts a Claude session, it opens a full-screen terminal UI:

- **Streaming output**: Claude's responses stream in real-time
- **Tool approval**: Write/edit/bash operations show a diff or command preview with Accept/Reject buttons
- **Auto-accept mode**: Press `a` during a session to toggle auto-accept, or use `--auto-accept` / `-y`
- **Pywal theme support**: Colors are loaded from `~/.cache/wal/colors.json` when available

### Keybindings

| Key | Action |
|-----|--------|
| Ctrl+C | Cancel and quit |
| a | Toggle auto-accept mode |
| Enter / y | Approve tool call |
| Esc / n | Reject tool call |
| Up / Down | Scroll viewport |
| Left / Right | Navigate Accept/Reject buttons |

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

Work directories persist between runs. On subsequent runs, hydra syncs the existing directory (fetch + reset to origin/HEAD) instead of re-cloning. If sync fails, the directory is removed and re-cloned.

## hydra.yml

Configure commands, model, and issue sync settings:

```yaml
# Model to use for Claude API calls (default: claude-sonnet-4-6)
model: claude-sonnet-4-6

# Issue sync API type: "github" or "gitea" (auto-detected from URL if omitted)
api_type: github

# Gitea instance URL (only needed if api_type is "gitea" and URL can't be parsed)
gitea_url: https://gitea.example.com

# Commands to run after Claude makes changes
commands:
  test: "go test ./... -count=1"
  lint: "golangci-lint run ./..."
```

Commands run in the task's work directory. A non-zero exit code fails the task. Undefined or commented-out commands are skipped.

## Document Assembly

When a task runs, hydra assembles a single markdown document sent to Claude:

```
# Rules
{contents of rules.md}

# Lint Rules
{contents of lint.md}

# Group
{contents of tasks/{group}/group.md}

# Task
{contents of the task .md file}

# Functional Tests
{contents of functional.md}
```

Sections with empty or missing source files are omitted entirely. The `# Group` section is only included for grouped tasks that have a `group.md` file.

## Project Layout

After `hydra init`, your project directory looks like:

```
project/
├── .hydra/
│   └── config.json        # Hydra configuration
├── work/                   # Per-task cloned repositories (created by hydra run)
└── design -> /path/to/design-dir  # Symlink to design directory
```

## License

MIT
