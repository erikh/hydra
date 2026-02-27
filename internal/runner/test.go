package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
)

// Test runs a test-focused session on a task in review state.
// Claude adds missing tests, runs test/lint commands, and fixes any issues.
// The task stays in review state after the session.
func (r *Runner) Test(taskName string) error {
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
	lk := lock.New(hydraDir, "test:"+taskName)
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

	// Rebase onto latest remote main if requested.
	if r.Rebase {
		conflictFiles, err := r.attemptRebase(taskRepo)
		if err != nil {
			return fmt.Errorf("rebasing onto main: %w", err)
		}
		if len(conflictFiles) > 0 {
			return fmt.Errorf("rebase conflicts — resolve manually before running tests: %v", conflictFiles)
		}
	}

	// Assemble a test-focused document.
	content, err := task.Content()
	if err != nil {
		return err
	}

	cmds := r.commandsMap(wd)
	doc := assembleTestDocument(content)

	// Append verification and commit instructions so Claude handles test/lint/staging/committing.
	sign := taskRepo.HasSigningKey()
	doc += verificationSection(cmds)
	doc += commitInstructions(sign, cmds)
	doc += missionReminder()

	// Run before hook.
	if err := r.runBeforeHook(wd); err != nil {
		return fmt.Errorf("before hook: %w", err)
	}

	// Capture HEAD before invoking Claude.
	beforeSHA, _ := taskRepo.LastCommitSHA()

	// Invoke Claude with test document.
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
		fmt.Printf("Test session for %q: no changes made.\n", taskName)
		return nil
	}

	// Record SHA and push.
	record := design.NewRecord(r.Config.DesignDir)
	if err := record.Add(afterSHA, "test:"+taskName); err != nil {
		return fmt.Errorf("recording SHA: %w", err)
	}

	if err := taskRepo.Push(branch); err != nil {
		if fpErr := taskRepo.ForcePushWithLease(branch); fpErr != nil {
			return fmt.Errorf("pushing: %w", fpErr)
		}
	}
	fmt.Printf("Test session for %q: tests added, committed, and pushed.\n", taskName)

	// Task stays in review state.
	return nil
}

// assembleTestDocument builds a document for the test session.
func assembleTestDocument(taskContent string) string {
	var b strings.Builder

	b.WriteString("# Mission\n\nYour sole objective is to add tests for the task described below. ")
	b.WriteString("Focus exclusively on identifying untested features from the task document and adding coverage. ")
	b.WriteString("Do not refactor existing code, add unrelated tests, or make changes outside the scope of this task.\n\n")

	b.WriteString("# Task Description\n\n")
	b.WriteString(taskContent)
	b.WriteString("\n\n")

	b.WriteString("# Test Instructions\n\n")
	b.WriteString("You are adding tests for an implementation of the above task. ")
	b.WriteString("Carefully read the task description and the existing implementation, ")
	b.WriteString("then follow these steps:\n\n")

	b.WriteString("1. Identify every feature, behavior, and edge case described in the task document\n")
	b.WriteString("2. Check which of these already have test coverage\n")
	b.WriteString("3. Add tests for any features or behaviors that lack coverage\n")
	b.WriteString("4. Ensure tests cover both success and error paths\n\n")

	return b.String()
}
