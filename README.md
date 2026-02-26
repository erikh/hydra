# Hydra

Drive development tasks from a tree of markdown design documents against a source code repository, using the `claude` CLI.

## Installation

```
go install github.com/erikh/hydra@latest
```

## Usage

### Initialize a project

```
hydra init <source-repo-url> <design-dir>
```

Clones the source repository and registers the design directory. Creates a `.hydra/` directory with configuration.

### List pending tasks

```
hydra list
```

### Show task status

```
hydra status
```

Shows tasks in each state (pending, review, merge, completed, abandoned) and any currently running task.

### Run a task

```
hydra run <task-name>
```

Executes the full task lifecycle:

1. Acquires a lock (only one task runs at a time)
2. Creates a branch `hydra/<task-name>`
3. Assembles a document from rules.md, lint.md, the task, and functional.md
4. Sends the document to `claude -p --dangerously-skip-permissions`
5. Commits and pushes changes
6. Moves the task to `state/review/`

## Design Directory Structure

```
rules.md           - Rules injected into every task context
lint.md            - Code quality and structure rules
functional.md      - Functional tests to exercise
tasks/             - Pending task files
tasks/{name}.md    - Individual task
tasks/{group}/     - Grouped tasks
state/review/      - Tasks finished, awaiting review
state/merge/       - Tasks reviewed, ready to merge
state/completed/   - Tasks that completed the full lifecycle
state/abandoned/   - Abandoned tasks
```

## License

MIT
