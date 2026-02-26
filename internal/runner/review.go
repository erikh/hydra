package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
)

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
	if err := claudeFn(context.Background(), runCfg); err != nil {
		return err
	}

	// If changes were made, commit and push.
	hasChanges, err := taskRepo.HasChanges()
	if err != nil {
		return fmt.Errorf("checking for changes: %w", err)
	}

	if hasChanges {
		if err := r.commitAndPushReview(taskRepo, taskName, branch); err != nil {
			return err
		}
		fmt.Printf("Review of %q: changes committed and pushed.\n", taskName)
	} else {
		fmt.Printf("Review of %q: no changes made.\n", taskName)
	}

	// Task stays in review state.
	return nil
}

// commitAndPushReview stages, commits, records, and pushes review changes.
func (r *Runner) commitAndPushReview(taskRepo *repo.Repo, taskName, branch string) error {
	if err := taskRepo.AddAll(); err != nil {
		return fmt.Errorf("staging changes: %w", err)
	}

	sign := taskRepo.HasSigningKey()
	commitMsg := "hydra: review " + taskName
	if err := taskRepo.Commit(commitMsg, sign); err != nil {
		return fmt.Errorf("committing: %w", err)
	}

	sha, err := taskRepo.LastCommitSHA()
	if err != nil {
		return fmt.Errorf("getting commit SHA: %w", err)
	}
	record := design.NewRecord(r.Config.DesignDir)
	if err := record.Add(sha, "review:"+taskName); err != nil {
		return fmt.Errorf("recording SHA: %w", err)
	}

	if err := taskRepo.Push(branch); err != nil {
		// Try force push with lease if normal push fails (rebased branch).
		if fpErr := taskRepo.ForcePushWithLease(branch); fpErr != nil {
			return fmt.Errorf("pushing: %w", fpErr)
		}
	}
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

	doc := ""
	if rules != "" {
		doc += "# Rules\n\n" + rules + "\n\n"
	}
	if lint != "" {
		doc += "# Lint Rules\n\n" + lint + "\n\n"
	}

	doc += "# Task\n\n" + taskContent + "\n\n"

	doc += "# Review Instructions\n\n"
	doc += "You are reviewing an implementation of the above task. " +
		"Please verify the implementation is correct, run any tests, " +
		"and make corrections as needed. Focus on:\n\n" +
		"- Correctness of the implementation\n" +
		"- Test coverage\n" +
		"- Code quality and adherence to the rules above\n" +
		"- Edge cases and error handling\n"

	// Add test/lint commands if configured.
	if r.TaskRunner != nil {
		if testCmd, ok := r.TaskRunner.Commands["test"]; ok {
			doc += fmt.Sprintf("\nRun tests with: `%s`\n", testCmd)
		}
		if lintCmd, ok := r.TaskRunner.Commands["lint"]; ok {
			doc += fmt.Sprintf("Run linter with: `%s`\n", lintCmd)
		}
	}

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
