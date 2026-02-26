// Package runner orchestrates the full hydra task lifecycle.
package runner

import (
	"errors"
	"fmt"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
)

// ClaudeFunc is the function signature for invoking claude.
// It receives the repo working directory and the assembled document.
type ClaudeFunc func(repoDir, document string) error

// Runner orchestrates the full hydra run workflow.
type Runner struct {
	Config  *config.Config
	Design  *design.Dir
	Repo    *repo.Repo
	Claude  ClaudeFunc
	BaseDir string // working directory for lock file; defaults to "."
}

// New creates a Runner from the given config.
func New(cfg *config.Config) (*Runner, error) {
	dd, err := design.NewDir(cfg.DesignDir)
	if err != nil {
		return nil, err
	}

	return &Runner{
		Config:  cfg,
		Design:  dd,
		Repo:    repo.Open(cfg.RepoDir),
		Claude:  invokeClaude,
		BaseDir: ".",
	}, nil
}

// Run executes the full task lifecycle: lock, branch, assemble, claude, commit, push, move to review.
func (r *Runner) Run(taskName string) error {
	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}
	hydraDir := config.HydraPath(baseDir)

	// Find the task
	task, err := r.Design.FindTask(taskName)
	if err != nil {
		return err
	}

	// Acquire lock
	lk := lock.New(hydraDir, taskName)
	if err := lk.Acquire(); err != nil {
		return err
	}
	defer func() { _ = lk.Release() }()

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

	doc, err := r.Design.AssembleDocument(content)
	if err != nil {
		return fmt.Errorf("assembling document: %w", err)
	}

	// Invoke claude
	claudeFn := r.Claude
	if claudeFn == nil {
		claudeFn = invokeClaude
	}
	if err := claudeFn(r.Config.RepoDir, doc); err != nil {
		return err
	}

	// Verify changes were made
	hasChanges, err := r.Repo.HasChanges()
	if err != nil {
		return fmt.Errorf("checking for changes: %w", err)
	}
	if !hasChanges {
		return errors.New("claude produced no changes")
	}

	// Commit and push
	if err := r.Repo.AddAll(); err != nil {
		return fmt.Errorf("staging changes: %w", err)
	}

	sign := r.Repo.HasSigningKey()
	commitMsg := "hydra: " + taskName
	if err := r.Repo.Commit(commitMsg, sign); err != nil {
		return fmt.Errorf("committing: %w", err)
	}

	if err := r.Repo.Push(branch); err != nil {
		return fmt.Errorf("pushing: %w", err)
	}

	// Move task to review
	if err := r.Design.MoveTask(task, design.StateReview); err != nil {
		return fmt.Errorf("moving task to review: %w", err)
	}

	fmt.Printf("Task %q completed successfully. Branch: %s\n", taskName, branch)
	return nil
}
