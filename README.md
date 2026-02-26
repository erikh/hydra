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

# Run the task (opens interactive TUI)
hydra run add-auth

# Run with auto-accept (no approval prompts)
hydra run -y add-auth

# Run all tasks in a group
hydra run-group backend

# Import issues from GitHub/Gitea
hydra sync

# Check progress
hydra status
```

All commands work from subdirectories — hydra searches upward for `.hydra/config.json`.

## Commands

### `hydra init <source-repo-url> <design-dir>`

Initializes a hydra project. Clones the source repository into `./repo`, registers the design directory, and creates `.hydra/config.json`. If the design directory is empty, scaffolds the full directory structure with placeholder files.

A convenience symlink `./design` is created pointing to the design directory.

### `hydra edit <task-name>`

Opens your editor to create a new task file. The editor is resolved from `$VISUAL`, then `$EDITOR`. The task name must not contain `/` (grouped task creation is not yet supported). If the editor exits with an error or the file is left empty, no task is created.

### `hydra run <task-name>`

Executes the full task lifecycle:

1. Finds the pending task by name (supports `group/name` for grouped tasks)
2. Acquires a file lock (`.hydra/hydra.lock`) — only one task runs at a time
3. Creates a git branch `hydra/<task-name>` (or `hydra/<group>/<task-name>`)
4. Assembles a document from `rules.md`, `lint.md`, the task content, and `functional.md`
5. Opens an interactive TUI session with the Anthropic API
6. Claude works through the task using tools (read, write, edit, bash, list, search)
7. Tool calls that modify files or run commands require approval (unless `--auto-accept` / `-y` is set)
8. Verifies Claude produced changes
9. Runs the `test` command from `hydra.yml` (if configured)
10. Runs the `lint` command from `hydra.yml` (if configured)
11. Stages all changes, commits (message: `hydra: <task-name>`), and pushes
12. Records the commit SHA in `state/record.json`
13. Moves the task file to `state/review/`
14. Releases the lock

Commits are GPG-signed if `user.signingkey` is configured in git. Stale locks from dead processes are automatically recovered.

**Flags:**

- `--auto-accept` / `-y`: Auto-accept all tool calls without prompting

### `hydra run-group <group-name>`

Executes all pending tasks in the named group sequentially, in alphabetical order by task name. Between tasks, the base branch is restored so each task starts from a clean state. Stops immediately on the first error.

If a `group.md` file exists in the group directory (`tasks/{group}/group.md`), its content is included as a `# Group` section in every task's assembled document.

**Flags:**

- `--auto-accept` / `-y`: Auto-accept all tool calls without prompting

### `hydra sync`

Imports open issues from GitHub or Gitea as task files under `tasks/issues/`.

- Issues are created as `tasks/issues/{number}-{slugified-title}.md`
- A `group.md` is created automatically for the issues group
- Existing issues (matched by number prefix) are skipped
- The API type is auto-detected from the source repo URL (GitHub if the host is `github.com`, Gitea otherwise), or can be set explicitly via `api_type` in `hydra.yml`

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

When `hydra run` starts a Claude session, it opens a full-screen terminal UI:

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
├── other/                            # Miscellaneous files
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

Commands run in the repo directory. A non-zero exit code fails the task. Undefined or commented-out commands are skipped.

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
├── repo/                   # Cloned source repository
└── design -> /path/to/design-dir  # Symlink to design directory
```

## License

MIT
