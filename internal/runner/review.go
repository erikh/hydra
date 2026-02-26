package runner

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
)

// ReviewDev runs the dev command from hydra.yml in the task's work directory.
// The process runs until it exits or the context is cancelled.
func (r *Runner) ReviewDev(ctx context.Context, taskName string) error {
	task, err := r.Design.FindTaskByState(taskName, design.StateReview)
	if err != nil {
		return err
	}

	wd := r.workDir(task)

	taskRepo, err := r.prepareRepo(wd)
	if err != nil {
		return fmt.Errorf("preparing work directory: %w", err)
	}

	branch := task.BranchName()
	if taskRepo.BranchExists(branch) {
		if err := taskRepo.Checkout(branch); err != nil {
			return fmt.Errorf("checking out branch: %w", err)
		}
	}

	if r.TaskRunner == nil {
		return errors.New("no dev command configured in hydra.yml and no dev target in Makefile")
	}

	err = r.TaskRunner.RunDev(ctx, wd)
	if err != nil && ctx.Err() != nil {
		fmt.Println("\nDev server stopped.")
		return nil //nolint:nilerr // intentional: replace signal error with friendly message
	}
	return err
}

// Review runs an interactive review session on a task in review state.
// The task stays in review state after the review session.
func (r *Runner) Review(taskName string) error {
	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}
	hydraDir := config.HydraPath(baseDir)

	// Find the task in review state.
	task, err := r.Design.FindTaskByState(taskName, design.StateReview)
	if err != nil {
		return err
	}

	// Acquire lock.
	lk := lock.New(hydraDir, "review:"+taskName)
	if err := lk.Acquire(); err != nil {
		return err
	}
	defer func() { _ = lk.Release() }()

	// Prepare work directory (should exist from run).
	wd := r.workDir(task)
	taskRepo, err := r.prepareRepo(wd)
	if err != nil {
		return fmt.Errorf("preparing work directory: %w", err)
	}

	// Checkout the task's branch.
	branch := task.BranchName()
	if !taskRepo.BranchExists(branch) {
		if err := taskRepo.CreateBranch(branch); err != nil {
			return fmt.Errorf("creating branch: %w", err)
		}
	} else {
		if err := taskRepo.Checkout(branch); err != nil {
			return fmt.Errorf("checking out branch: %w", err)
		}
	}

	// Assemble a review-focused document.
	content, err := task.Content()
	if err != nil {
		return err
	}

	doc, err := r.assembleReviewDocument(content)
	if err != nil {
		return fmt.Errorf("assembling review document: %w", err)
	}

	// Append verification and commit instructions so Claude handles test/lint/staging/committing.
	sign := taskRepo.HasSigningKey()
	cmds := r.commandsMap(wd)
	doc += verificationSection(cmds)
	doc += commitInstructions(sign, cmds)
	doc += missionReminder()

	// Run before hook.
	if err := r.runBeforeHook(wd); err != nil {
		return fmt.Errorf("before hook: %w", err)
	}

	// Capture HEAD before invoking Claude.
	beforeSHA, _ := taskRepo.LastCommitSHA()

	// Invoke Claude with review document.
	claudeFn := r.Claude
	if claudeFn == nil {
		claudeFn = invokeClaude
	}
	runCfg := ClaudeRunConfig{
		RepoDir:    taskRepo.Dir,
		Document:   doc,
		Model:      r.Model,
		AutoAccept: r.AutoAccept,
		PlanMode:   r.PlanMode,
	}
	claudeErr := claudeFn(context.Background(), runCfg)

	// Check if Claude committed (HEAD moved), even if Claude returned an error
	// (e.g. terminated by signal after committing).
	afterSHA, _ := taskRepo.LastCommitSHA()

	if afterSHA == beforeSHA {
		if claudeErr != nil {
			return claudeErr
		}
		fmt.Printf("Review of %q: no changes made.\n", taskName)
		return nil
	}

	// Record SHA and push.
	record := design.NewRecord(r.Config.DesignDir)
	if err := record.Add(afterSHA, "review:"+taskName); err != nil {
		return fmt.Errorf("recording SHA: %w", err)
	}

	if err := taskRepo.Push(branch); err != nil {
		// Try force push with lease if normal push fails (rebased branch).
		if fpErr := taskRepo.ForcePushWithLease(branch); fpErr != nil {
			return fmt.Errorf("pushing: %w", fpErr)
		}
	}
	fmt.Printf("Review of %q: changes committed and pushed.\n", taskName)

	// Task stays in review state.
	return nil
}

// assembleReviewDocument builds a document for the review session.
func (r *Runner) assembleReviewDocument(taskContent string) (string, error) {
	rules, err := r.Design.Rules()
	if err != nil {
		return "", err
	}

	lint, err := r.Design.Lint()
	if err != nil {
		return "", err
	}

	doc := "# Mission\n\nYour sole objective is to review the implementation of the task described below. " +
		"Focus exclusively on verifying correctness, test coverage, and commit messages for this specific task. " +
		"Do not make unrelated improvements or refactor code outside the task's scope.\n\n"
	if rules != "" {
		doc += "# Rules\n\n" + rules + "\n\n"
	}
	if lint != "" {
		doc += "# Lint Rules\n\n" + lint + "\n\n"
	}

	doc += "# Task\n\n" + taskContent + "\n\n"

	doc += "# Review Instructions\n\n"
	doc += "You are reviewing an implementation of the above task. " +
		"Please verify the implementation is correct and make corrections as needed. Focus on:\n\n" +
		"- Correctness of the implementation\n" +
		"- Code quality and adherence to the rules above\n" +
		"- Edge cases and error handling\n\n"

	doc += "## Commit Message Validation\n\n"
	doc += "Read the git log and verify that the commit message(s) accurately describe " +
		"the changes made. Compare them against the task document above. " +
		"If the commit messages are vague, misleading, or do not reflect the actual changes, " +
		"amend the most recent commit with a corrected message.\n\n"

	doc += "## Test Coverage Validation\n\n"
	doc += "Carefully read the task document above and identify every feature, behavior, or change it describes. " +
		"Verify that each item has corresponding test coverage. " +
		"If any described feature or behavior lacks tests, add the missing tests. " +
		"Every testable requirement in the task document must have at least one test.\n"

	return doc, nil
}

// ReviewList prints tasks in review state.
func (r *Runner) ReviewList() error {
	tasks, err := r.Design.TasksByState(design.StateReview)
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks in review.")
		return nil
	}

	for _, t := range tasks {
		label := t.Name
		if t.Group != "" {
			label = t.Group + "/" + t.Name
		}
		fmt.Println(label)
	}
	return nil
}

// ReviewView prints the content of a task in review state.
func (r *Runner) ReviewView(taskName string) error {
	task, err := r.Design.FindTaskByState(taskName, design.StateReview)
	if err != nil {
		return err
	}

	content, err := task.Content()
	if err != nil {
		return err
	}

	fmt.Print(content)
	return nil
}

// ReviewEdit opens a task in review state in the editor.
func (r *Runner) ReviewEdit(taskName, editor string) error {
	task, err := r.Design.FindTaskByState(taskName, design.StateReview)
	if err != nil {
		return err
	}

	return design.RunEditorOnFile(editor, task.FilePath, os.Stdin, os.Stdout, os.Stderr)
}

// ReviewRemove moves a task from review to abandoned.
func (r *Runner) ReviewRemove(taskName string) error {
	task, err := r.Design.FindTaskByState(taskName, design.StateReview)
	if err != nil {
		return err
	}

	return r.Design.MoveTask(task, design.StateAbandoned)
}
