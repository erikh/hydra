// Package runner orchestrates the full hydra task lifecycle.
package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
	"github.com/erikh/hydra/internal/taskrun"
)

// ClaudeFunc is the function signature for invoking claude.
// It receives the repo working directory and the assembled document.
type ClaudeFunc func(repoDir, document string) error

// Runner orchestrates the full hydra run workflow.
type Runner struct {
	Config     *config.Config
	Design     *design.Dir
	Repo       *repo.Repo
	Claude     ClaudeFunc
	TaskRunner *taskrun.Commands // loaded from hydra.yml; nil if not present
	BaseDir    string            // working directory for lock file; defaults to "."
}

// New creates a Runner from the given config.
func New(cfg *config.Config) (*Runner, error) {
	dd, err := design.NewDir(cfg.DesignDir)
	if err != nil {
		return nil, err
	}

	r := &Runner{
		Config:  cfg,
		Design:  dd,
		Repo:    repo.Open(cfg.RepoDir),
		Claude:  invokeClaude,
		BaseDir: ".",
	}

	// Load hydra.yml if it exists.
	ymlPath := filepath.Join(cfg.DesignDir, "hydra.yml")
	if _, err := os.Stat(ymlPath); err == nil {
		cmds, err := taskrun.Load(ymlPath)
		if err != nil {
			return nil, fmt.Errorf("loading hydra.yml: %w", err)
		}
		r.TaskRunner = cmds
	}

	return r, nil
}

// Run executes the full task lifecycle: lock, branch, assemble, claude, test, lint, commit, push, record, move to review.
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

	groupContent, err := r.Design.GroupContent(task.Group)
	if err != nil {
		return fmt.Errorf("reading group content: %w", err)
	}

	doc, err := r.Design.AssembleDocument(content, groupContent)
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

	// Run test and lint commands if configured
	if r.TaskRunner != nil {
		if err := r.TaskRunner.Run("test", r.Config.RepoDir); err != nil {
			return fmt.Errorf("test step failed: %w", err)
		}
		if err := r.TaskRunner.Run("lint", r.Config.RepoDir); err != nil {
			return fmt.Errorf("lint step failed: %w", err)
		}
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

	// Record SHA -> task name
	sha, err := r.Repo.LastCommitSHA()
	if err != nil {
		return fmt.Errorf("getting commit SHA: %w", err)
	}
	record := design.NewRecord(r.Config.DesignDir)
	if err := record.Add(sha, taskName); err != nil {
		return fmt.Errorf("recording SHA: %w", err)
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

// RunGroup executes all pending tasks in a group sequentially.
// Between tasks, it checks out the base branch so each task starts from a clean state.
func (r *Runner) RunGroup(groupName string) error {
	baseBranch, err := r.Repo.CurrentBranch()
	if err != nil {
		return fmt.Errorf("getting current branch: %w", err)
	}

	tasks, err := r.Design.PendingTasks()
	if err != nil {
		return fmt.Errorf("listing pending tasks: %w", err)
	}

	var groupTasks []design.Task
	for _, t := range tasks {
		if t.Group == groupName {
			groupTasks = append(groupTasks, t)
		}
	}

	if len(groupTasks) == 0 {
		return fmt.Errorf("no pending tasks found in group %q", groupName)
	}

	sort.Slice(groupTasks, func(i, j int) bool {
		return groupTasks[i].Name < groupTasks[j].Name
	})

	for i, t := range groupTasks {
		taskRef := groupName + "/" + t.Name
		if err := r.Run(taskRef); err != nil {
			return fmt.Errorf("task %s: %w", taskRef, err)
		}

		// Reset to base branch between tasks (skip after last task).
		if i < len(groupTasks)-1 {
			if err := r.Repo.Checkout(baseBranch); err != nil {
				return fmt.Errorf("checking out base branch: %w", err)
			}
		}
	}

	return nil
}
