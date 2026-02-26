# Hydra

Local pull request workflow where Claude is the only contributor.

Hydra turns a tree of markdown design documents into branches, code, and commits — without you writing a line. It assembles context from your design docs, hands it to Claude, runs tests and linting, and pushes a branch ready for your review.

## Installation

```
go install github.com/erikh/hydra@latest
```

Requires the [Claude CLI](https://docs.anthropic.com/en/docs/claude-cli) to be installed and configured.

## Quick Start

```sh
# Initialize a project with a source repo and design directory
hydra init https://github.com/you/project.git ./my-design

# Create a new task
hydra edit add-auth

# Run the task
hydra run add-auth

# Check progress
hydra status
```

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
5. Sends the document to `claude -p --dangerously-skip-permissions` in the repo directory
6. Verifies Claude produced changes
7. Runs the `test` command from `hydra.yml` (if configured)
8. Runs the `lint` command from `hydra.yml` (if configured)
9. Stages all changes, commits (message: `hydra: <task-name>`), and pushes
10. Records the commit SHA in `state/record.json`
11. Moves the task file to `state/review/`
12. Releases the lock

Commits are GPG-signed if `user.signingkey` is configured in git. Stale locks from dead processes are automatically recovered.

### `hydra list`

Lists all pending tasks from the `tasks/` directory. Grouped tasks are displayed as `group/name`.

### `hydra status`

Shows tasks grouped by state (pending, review, merge, completed, abandoned) and displays any currently running task with its PID.

### `hydra milestone`

Lists milestone targets from the `milestone/` directory and historical milestone scores from `milestone/history/`. History entries include a letter grade (A-F) parsed from the filename.

## Design Directory Structure

```
design-dir/
├── rules.md                          # Rules injected into every task context
├── lint.md                           # Code quality and linting rules
├── functional.md                     # Functional test requirements
├── hydra.yml                         # Test and lint command configuration
├── tasks/                            # Pending task files
│   ├── {name}.md                     # Individual task
│   └── {group}/                      # Task group (subdirectory)
│       └── {name}.md                 # Grouped task
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

Configure test and lint commands that run after Claude makes changes:

```yaml
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

# Task
{contents of the task .md file}

# Functional Tests
{contents of functional.md}
```

Sections with empty or missing source files are omitted entirely.

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
