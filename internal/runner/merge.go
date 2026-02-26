package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/issues"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
)

// Merge runs the merge workflow: rebase onto origin/main, test, rebase into main, push.
// Accepts tasks in review or merge state (merge state for retries).
func (r *Runner) Merge(taskName string) error {
	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}
	hydraDir := config.HydraPath(baseDir)

	// Find task in review or merge state.
	task, err := r.findMergeTask(taskName)
	if err != nil {
		return err
	}

	// Move to merge state if not already there.
	if task.State != design.StateMerge {
		if err := r.Design.MoveTask(task, design.StateMerge); err != nil {
			return fmt.Errorf("moving task to merge state: %w", err)
		}
	}

	// Acquire lock.
	lk := lock.New(hydraDir, "merge:"+taskName)
	if err := lk.Acquire(); err != nil {
		return err
	}
	defer func() { _ = lk.Release() }()

	// Prepare work directory.
	wd := r.workDir(task)
	taskRepo, err := r.prepareRepo(wd)
	if err != nil {
		return fmt.Errorf("preparing work directory: %w", err)
	}

	// Checkout the task's branch.
	branch := task.BranchName()
	if !taskRepo.BranchExists(branch) {
		return fmt.Errorf("task branch %q does not exist", branch)
	}
	if err := taskRepo.Checkout(branch); err != nil {
		return fmt.Errorf("checking out branch: %w", err)
	}

	// Rebase loop: repeat until task branch is on top of origin/main.
	if err := r.rebaseLoop(task, taskRepo); err != nil {
		return err
	}

	// Pre-merge verification: double-check commit messages, tests, and lint.
	if err := r.preMergeChecks(task, taskRepo); err != nil {
		return err
	}

	// Rebase task branch into main and push.
	defaultBranch, err := r.rebaseAndPush(taskRepo, branch)
	if err != nil {
		return err
	}

	// Record SHA, complete task, close issue.
	return r.finalizeMerge(task, taskRepo, taskName, defaultBranch)
}

// findMergeTask locates a task in review or merge state.
func (r *Runner) findMergeTask(taskName string) (*design.Task, error) {
	task, err := r.Design.FindTaskByState(taskName, design.StateReview)
	if err == nil {
		return task, nil
	}
	task, err = r.Design.FindTaskByState(taskName, design.StateMerge)
	if err != nil {
		return nil, fmt.Errorf("task %q not found in review or merge state", taskName)
	}
	return task, nil
}

// rebaseLoop rebases the task branch onto origin/main, resolving conflicts
// and fixing test failures as needed. Returns when the branch is up-to-date.
func (r *Runner) rebaseLoop(task *design.Task, taskRepo *repo.Repo) error {
	for {
		if err := taskRepo.Fetch(); err != nil {
			return fmt.Errorf("fetching: %w", err)
		}

		defaultBranch, err := r.detectDefaultBranch(taskRepo)
		if err != nil {
			return fmt.Errorf("detecting default branch: %w", err)
		}
		originRef := "origin/" + defaultBranch

		if taskRepo.IsAncestor(originRef, "HEAD") {
			return nil
		}

		if err := r.rebaseOnto(task, taskRepo, originRef); err != nil {
			return err
		}

		if err := r.runPostRebaseChecks(task, taskRepo); err != nil {
			return err
		}
	}
}

// rebaseOnto attempts a rebase and handles conflicts via Claude.
func (r *Runner) rebaseOnto(task *design.Task, taskRepo *repo.Repo, originRef string) error {
	if err := taskRepo.Rebase(originRef); err == nil {
		return nil
	}

	// Rebase failed — likely conflicts. Open TUI for Claude to resolve.
	conflictFiles, _ := taskRepo.ConflictFiles()
	content, _ := task.Content()
	logOutput, _ := taskRepo.Log(10)

	doc := r.assembleConflictDocument(content, conflictFiles, logOutput)
	if err := r.invokeClaudeForRepo(taskRepo.Dir, doc); err != nil {
		_ = taskRepo.RebaseAbort()
		return fmt.Errorf("conflict resolution failed: %w", err)
	}

	if err := taskRepo.AddAll(); err != nil {
		_ = taskRepo.RebaseAbort()
		return fmt.Errorf("staging resolved files: %w", err)
	}
	if err := taskRepo.RebaseContinue(); err != nil {
		_ = taskRepo.RebaseAbort()
		return fmt.Errorf("rebase continue failed: %w", err)
	}
	return nil
}

// runPostRebaseChecks runs test and lint after a successful rebase,
// invoking Claude to fix test failures if needed.
func (r *Runner) runPostRebaseChecks(task *design.Task, taskRepo *repo.Repo) error {
	if r.TaskRunner == nil {
		return nil
	}

	if err := r.TaskRunner.Run("test", taskRepo.Dir); err != nil {
		if fixErr := r.fixTestFailures(task, taskRepo, err.Error()); fixErr != nil {
			return fixErr
		}
	}
	if err := r.TaskRunner.Run("lint", taskRepo.Dir); err != nil {
		return fmt.Errorf("lint failed after rebase: %w", err)
	}
	return nil
}

// fixTestFailures opens a Claude TUI to fix test failures, then commits fixes.
func (r *Runner) fixTestFailures(task *design.Task, taskRepo *repo.Repo, testError string) error {
	content, _ := task.Content()
	doc := r.assembleTestFixDocument(content, testError)

	// Append commit instructions without test commands (the point is to fix failures).
	sign := taskRepo.HasSigningKey()
	doc += commitInstructions(sign, nil)

	// Capture HEAD before invoking Claude.
	beforeSHA, _ := taskRepo.LastCommitSHA()

	if err := r.invokeClaudeForRepo(taskRepo.Dir, doc); err != nil {
		return fmt.Errorf("test fix failed: %w", err)
	}

	// Check if Claude committed.
	afterSHA, _ := taskRepo.LastCommitSHA()
	if afterSHA != beforeSHA {
		return nil
	}

	// Fallback: if Claude didn't commit, commit any changes ourselves.
	hasChanges, _ := taskRepo.HasChanges()
	if !hasChanges {
		return nil
	}

	if err := taskRepo.AddAll(); err != nil {
		return fmt.Errorf("staging test fixes: %w", err)
	}
	if err := taskRepo.Commit("hydra: fix tests after rebase", sign); err != nil {
		return fmt.Errorf("committing test fixes: %w", err)
	}
	return nil
}

// invokeClaudeForRepo invokes the Claude function with the given repo dir and document.
func (r *Runner) invokeClaudeForRepo(repoDir, document string) error {
	claudeFn := r.Claude
	if claudeFn == nil {
		claudeFn = invokeClaude
	}
	return claudeFn(context.Background(), ClaudeRunConfig{
		RepoDir:    repoDir,
		Document:   document,
		Model:      r.Model,
		AutoAccept: r.AutoAccept,
		PlanMode:   r.PlanMode,
	})
}

// preMergeChecks invokes Claude to verify commit messages, test coverage, and lint
// before merging into the default branch.
func (r *Runner) preMergeChecks(task *design.Task, taskRepo *repo.Repo) error {
	content, _ := task.Content()
	doc := r.assemblePreMergeDocument(content)

	sign := taskRepo.HasSigningKey()
	cmds := r.commandsMap()
	doc += commitInstructions(sign, cmds)

	beforeSHA, _ := taskRepo.LastCommitSHA()

	if err := r.invokeClaudeForRepo(taskRepo.Dir, doc); err != nil {
		return fmt.Errorf("pre-merge checks failed: %w", err)
	}

	// If Claude committed fixes, push the updated branch.
	afterSHA, _ := taskRepo.LastCommitSHA()
	if afterSHA != beforeSHA {
		branch := task.BranchName()
		if err := taskRepo.ForcePushWithLease(branch); err != nil {
			return fmt.Errorf("pushing pre-merge fixes: %w", err)
		}
	}

	return nil
}

// assemblePreMergeDocument builds a document for pre-merge verification.
func (r *Runner) assemblePreMergeDocument(taskContent string) string {
	var b strings.Builder
	b.WriteString("# Pre-Merge Verification\n\n")
	b.WriteString("This branch is about to be merged into the default branch. " +
		"Perform a final verification before merge.\n\n")

	b.WriteString("## Task Document\n\n")
	b.WriteString(taskContent)
	b.WriteString("\n\n")

	b.WriteString("## Checks to Perform\n\n")

	b.WriteString("### 1. Commit Message Validation\n\n")
	b.WriteString("Read the git log for this branch. Verify that the commit message(s) " +
		"accurately describe the changes made according to the task document above. " +
		"If any commit message is vague, misleading, or does not reflect the actual changes, " +
		"amend the most recent commit with a corrected message.\n\n")

	b.WriteString("### 2. Test Coverage\n\n")
	b.WriteString("Verify that every feature, behavior, or change described in the task document " +
		"has corresponding test coverage. If any requirement lacks tests, add the missing tests.\n\n")

	b.WriteString("### 3. Lint\n\n")
	if r.TaskRunner != nil {
		if lintCmd, ok := r.TaskRunner.Commands["lint"]; ok {
			b.WriteString(fmt.Sprintf("Run the linter: `%s`\n", lintCmd))
			b.WriteString("Fix any lint issues found.\n\n")
		}
	}

	b.WriteString("### 4. Tests\n\n")
	if r.TaskRunner != nil {
		if testCmd, ok := r.TaskRunner.Commands["test"]; ok {
			b.WriteString(fmt.Sprintf("Run the test suite: `%s`\n", testCmd))
			b.WriteString("Fix any test failures.\n\n")
		}
	}

	b.WriteString("If everything passes and no changes are needed, do not create a commit.\n")

	return b.String()
}

// rebaseAndPush checks out the default branch, rebases the task branch into it, and pushes.
func (r *Runner) rebaseAndPush(taskRepo *repo.Repo, branch string) (string, error) {
	defaultBranch, err := r.detectDefaultBranch(taskRepo)
	if err != nil {
		return "", fmt.Errorf("detecting default branch: %w", err)
	}

	if err := taskRepo.Checkout(defaultBranch); err != nil {
		return "", fmt.Errorf("checking out %s: %w", defaultBranch, err)
	}

	if err := taskRepo.Fetch(); err != nil {
		return "", fmt.Errorf("fetching before rebase: %w", err)
	}

	if err := taskRepo.Rebase(branch); err != nil {
		return "", fmt.Errorf("rebase failed: %w", err)
	}

	if err := taskRepo.PushMain(); err != nil {
		return "", fmt.Errorf("pushing main: %w", err)
	}

	return defaultBranch, nil
}

// finalizeMerge records the SHA, moves the task to completed, and closes the issue.
func (r *Runner) finalizeMerge(task *design.Task, taskRepo *repo.Repo, taskName, defaultBranch string) error {
	sha, err := taskRepo.LastCommitSHA()
	if err != nil {
		return fmt.Errorf("getting commit SHA: %w", err)
	}
	record := design.NewRecord(r.Config.DesignDir)
	if err := record.Add(sha, "merge:"+taskName); err != nil {
		return fmt.Errorf("recording SHA: %w", err)
	}

	if err := r.Design.MoveTask(task, design.StateCompleted); err != nil {
		return fmt.Errorf("moving task to completed: %w", err)
	}

	r.closeIssueIfNeeded(task, sha)

	fmt.Printf("Task %q merged to %s and pushed. SHA: %s\n", taskName, defaultBranch, sha[:12])
	return nil
}

// detectDefaultBranch returns the default branch name (main or master).
func (r *Runner) detectDefaultBranch(taskRepo interface{ BranchExists(string) bool }) (string, error) {
	if taskRepo.BranchExists("origin/main") {
		return "main", nil
	}
	if taskRepo.BranchExists("origin/master") {
		return "master", nil
	}
	return "", errors.New("cannot detect default branch (neither main nor master found)")
}

// assembleConflictDocument builds a document for conflict resolution.
func (r *Runner) assembleConflictDocument(taskContent string, conflictFiles []string, recentLog string) string {
	var b strings.Builder
	b.WriteString("# Conflict Resolution\n\n")
	b.WriteString("A rebase onto origin/main has resulted in merge conflicts. " +
		"Please resolve all conflicts in the files listed below.\n\n")

	if len(conflictFiles) > 0 {
		b.WriteString("## Conflicted Files\n\n")
		for _, f := range conflictFiles {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Task Context\n\n")
	b.WriteString(taskContent)
	b.WriteString("\n\n")

	if recentLog != "" {
		b.WriteString("## Recent Commits on origin/main\n\n```\n")
		b.WriteString(recentLog)
		b.WriteString("\n```\n\n")
	}

	b.WriteString("After resolving conflicts, stage the resolved files with `git add`.\n")
	return b.String()
}

// assembleTestFixDocument builds a document for fixing test failures after rebase.
func (r *Runner) assembleTestFixDocument(taskContent string, testError string) string {
	doc := "# Fix Test Failures\n\n"
	doc += "Tests failed after rebasing onto origin/main. " +
		"Please fix the test failures.\n\n"
	doc += "## Error Output\n\n```\n" + testError + "\n```\n\n"
	doc += "## Task Context\n\n" + taskContent + "\n"

	if r.TaskRunner != nil {
		if testCmd, ok := r.TaskRunner.Commands["test"]; ok {
			doc += fmt.Sprintf("\nRun tests with: `%s`\n", testCmd)
		}
	}

	return doc
}

// closeIssueIfNeeded closes the remote issue if the task is an issue task.
func (r *Runner) closeIssueIfNeeded(task *design.Task, sha string) {
	if r.IssueCloser == nil || !issues.IsIssueTask(task) {
		return
	}
	num := issues.ParseIssueTaskNumber(task.Name)
	if num == 0 {
		return
	}
	comment := "Closed by hydra. Commit: " + sha
	if err := r.IssueCloser.CloseIssue(num, comment); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not close issue #%d: %v\n", num, err)
	}
}

// MergeList prints tasks in merge state.
func (r *Runner) MergeList() error {
	tasks, err := r.Design.TasksByState(design.StateMerge)
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks in merge state.")
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

// MergeView prints the content of a task in merge state.
func (r *Runner) MergeView(taskName string) error {
	task, err := r.Design.FindTaskByState(taskName, design.StateMerge)
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

// MergeEdit opens a task in merge state in the editor.
func (r *Runner) MergeEdit(taskName, editor string) error {
	task, err := r.Design.FindTaskByState(taskName, design.StateMerge)
	if err != nil {
		return err
	}

	return design.RunEditorOnFile(editor, task.FilePath, os.Stdin, os.Stdout, os.Stderr)
}

// MergeRemove moves a task from merge to abandoned.
func (r *Runner) MergeRemove(taskName string) error {
	task, err := r.Design.FindTaskByState(taskName, design.StateMerge)
	if err != nil {
		return err
	}

	return r.Design.MoveTask(task, design.StateAbandoned)
}
