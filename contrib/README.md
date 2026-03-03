# contrib

Community-contributed helpers and integrations for Hydra.

## Makefile.tmux

A Makefile for running hydra tasks in parallel using tmux windows. Each target spawns a new tmux window per task, letting you monitor all running tasks from your tmux status bar.

Hydra sets the xterm title during every command it runs. Tmux automatically picks this up as the window name, so each window in your session is labeled with what hydra is doing (e.g. `run:add-feature`, `merge:fix-bug`).

### Targets

| Target | Description |
|--------|-------------|
| `run-all` | Spawn a window for every pending task and run `hydra run` |
| `review-all` | Spawn a window for every review-state task and run `hydra review run` |
| `merge-all` | Spawn a window for every review/merge-state task and run `hydra merge run` |
| `test-all` | Spawn a window for every review-state task and run `hydra test` |

### Usage

Copy it into your hydra project directory and run from inside a tmux session:

```sh
cp contrib/Makefile.tmux ./Makefile
make run-all
```

Or invoke it directly without copying:

```sh
make -f contrib/Makefile.tmux run-all
```
