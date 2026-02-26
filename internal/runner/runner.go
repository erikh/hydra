package runner

import (
	"fmt"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
)

type Runner struct {
	Config    *config.Config
	DesignDir *design.DesignDir
	Repo      *repo.Repo
}

func New(cfg *config.Config) (*Runner, error) {
	dd, err := design.NewDesignDir(cfg.DesignDir)
	if err != nil {
		return nil, err
	}

	return &Runner{
		Config:    cfg,
		DesignDir: dd,
		Repo:      repo.Open(cfg.RepoDir),
	}, nil
}

func (r *Runner) Run(taskName string) error {
	hydraDir := config.HydraPath(".")

	// Find the task
	task, err := r.DesignDir.FindTask(taskName)
	if err != nil {
		return err
	}

	// Acquire lock
	lk := lock.New(hydraDir, taskName)
	if err := lk.Acquire(); err != nil {
		return err
	}
	defer lk.Release()

	// Create and checkout branch
	branch := task.BranchName()
	if err := r.Repo.CreateBranch(branch); err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}

	// Read task content and assemble document
	content, err := task.Content()
	if err != nil {
		return err
	}

	doc, err := r.DesignDir.AssembleDocument(content)
	if err != nil {
		return fmt.Errorf("assembling document: %w", err)
	}

	// Invoke claude
	if err := invokeClaude(r.Config.RepoDir, doc); err != nil {
		return err
	}

	// Verify changes were made
	hasChanges, err := r.Repo.HasChanges()
	if err != nil {
		return fmt.Errorf("checking for changes: %w", err)
	}
	if !hasChanges {
		return fmt.Errorf("claude produced no changes")
	}

	// Commit and push
	if err := r.Repo.AddAll(); err != nil {
		return fmt.Errorf("staging changes: %w", err)
	}

	sign := r.Repo.HasSigningKey()
	commitMsg := fmt.Sprintf("hydra: %s", taskName)
	if err := r.Repo.Commit(commitMsg, sign); err != nil {
		return fmt.Errorf("committing: %w", err)
	}

	if err := r.Repo.Push(branch); err != nil {
		return fmt.Errorf("pushing: %w", err)
	}

	// Move task to review
	if err := r.DesignDir.MoveTask(task, design.StateReview); err != nil {
		return fmt.Errorf("moving task to review: %w", err)
	}

	fmt.Printf("Task %q completed successfully. Branch: %s\n", taskName, branch)
	return nil
}
