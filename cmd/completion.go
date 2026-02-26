package cmd

import (
	"fmt"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/urfave/cli/v2"
)

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
