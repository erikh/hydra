package runner

import (
	"errors"
	"fmt"
)

// Clean runs the clean command in the task's work directory.
// Uses the clean command from hydra.yml, or falls back to "make clean"
// if a Makefile with a clean target exists. The task can be in any state.
func (r *Runner) Clean(taskName string) error {
	task, err := r.Design.FindTaskAny(taskName)
	if err != nil {
		return err
	}

	wd := r.workDir(task)

	if r.TaskRunner == nil || !r.TaskRunner.HasCommand("clean", wd) {
		return errors.New("no clean command configured in hydra.yml and no clean target in Makefile")
	}

	if err := r.TaskRunner.Run("clean", wd); err != nil {
		return fmt.Errorf("clean failed: %w", err)
	}

	return nil
}
