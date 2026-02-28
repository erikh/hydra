package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/issues"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
)

// Merge runs the merge workflow:
//  1. Fetch origin, checkout task branch, abort any in-progress rebase
//  2. Rebase task branch onto origin/main
//  3. If conflicts, invoke Claude to resolve them
//  4. Force-push the branch
//  5. Checkout main, rebase against origin/main, rebase against feature branch, push
//
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

	// Step 1: Checkout the task's branch (skip if working tree is dirty).
	branch := task.BranchName()
	if !taskRepo.BranchExists(branch) {
		return fmt.Errorf("task branch %q does not exist", branch)
	}
	dirty, err := taskRepo.HasChanges()
	if err != nil {
		return fmt.Errorf("checking working tree: %w", err)
	}
	if !dirty {
		if err := taskRepo.Checkout(branch); err != nil {
			return fmt.Errorf("checking out branch: %w", err)
		}
	}

	// Step 3: Abort any in-progress rebase from a previous failed attempt.
	if err := taskRepo.RebaseAbort(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: rebase abort failed: %v\n", err)
	}

	// Step 4: Rebase task branch onto origin/main; collect conflict info if any.
	// Skip rebase if the working tree is dirty — let Claude handle it.
	var conflictFiles []string
	dirty, err = taskRepo.HasChanges()
	if err != nil {
		return fmt.Errorf("checking working tree: %w", err)
	}
	if !dirty {
		conflictFiles, err = r.attemptRebase(taskRepo)
		if err != nil {
			return err
		}
	}

	// Step 5: Assemble document and invoke Claude.
	content, err := task.Content()
	if err != nil {
		return fmt.Errorf("reading task content: %w", err)
	}
	cmds := r.commandsMap(wd)
	sign := taskRepo.HasSigningKey()
	doc, err := r.assembleMergeDocument(content, conflictFiles, cmds, sign, r.timeout(), r.Notify, r.notifyTitle(taskName))
	if err != nil {
		return fmt.Errorf("assembling merge document: %w", err)
	}

	// Run before hook.
	if err := r.runBeforeHook(wd); err != nil {
		return fmt.Errorf("before hook: %w", err)
	}

	claudeFn := r.Claude
	if claudeFn == nil {
		claudeFn = invokeClaude
	}
	if err := claudeFn(context.Background(), ClaudeRunConfig{
		RepoDir:    taskRepo.Dir,
		Document:   doc,
		Model:      r.Model,
		AutoAccept: r.AutoAccept,
		PlanMode:   r.PlanMode,
	}); err != nil {
		return fmt.Errorf("claude failed: %w", err)
	}

	// Step 6: Force-push the branch (Claude may have added commits).
	if err := taskRepo.ForcePushWithLease(branch); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	// Step 7: Checkout main, rebase against origin/main, then against feature branch, push.
	defaultBranch, err := r.rebaseAndPush(taskRepo, branch)
	if err != nil {
		return err
	}

	// Step 8: Record SHA, complete task, close issue, clean up remote branch.
	return r.finalizeMerge(task, taskRepo, taskName, branch, defaultBranch)
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

// attemptRebase fetches origin, detects the default branch, and attempts to
// rebase onto origin/<default>. If the rebase has conflicts, it aborts the
// rebase and returns the list of conflicted files. On success, returns an
// empty list.
func (r *Runner) attemptRebase(taskRepo *repo.Repo) ([]string, error) {
	// Always fetch origin before rebasing to ensure we have latest refs.
	if err := taskRepo.Fetch(); err != nil {
		return nil, fmt.Errorf("fetching origin before rebase: %w", err)
	}

	defaultBranch, err := r.detectDefaultBranch(taskRepo)
	if err != nil {
		return nil, fmt.Errorf("detecting default branch: %w", err)
	}
	originRef := "origin/" + defaultBranch

	// Already up-to-date.
	if taskRepo.IsAncestor(originRef, "HEAD") {
		return nil, nil
	}

	// Try the rebase.
	if err := taskRepo.Rebase(originRef); err == nil {
		return nil, nil
	}

	// Rebase failed — collect conflict files and abort.
	conflictFiles, cfErr := taskRepo.ConflictFiles()
	if cfErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not list conflict files: %v\n", cfErr)
	}
	if err := taskRepo.RebaseAbort(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: rebase abort failed: %v\n", err)
	}
	return conflictFiles, nil
}

// assembleMergeDocument builds a single comprehensive document for the merge
// workflow. It covers conflict resolution (if needed), test/lint verification,
// commit message validation, and test coverage — all in one Claude session.
//
// The calling tool handles all git orchestration (fetch, rebase, checkout, push).
// Claude's job is limited to: resolving conflicts (if any), validating commits,
// verifying test coverage, and running tests.
func (r *Runner) assembleMergeDocument(taskContent string, conflictFiles []string, cmds map[string]string, sign bool, timeout time.Duration, notify bool, notifyTitle string) (string, error) {
	rules, err := r.Design.Rules()
	if err != nil {
		return "", err
	}

	lint, err := r.Design.Lint()
	if err != nil {
		return "", err
	}

	var b strings.Builder

	b.WriteString("# Merge Workflow\n\n")
	b.WriteString("This feature branch is being prepared for merge into the default branch. " +
		"You are on the feature branch. Stay on it — do NOT checkout main or any other branch. " +
		"Do NOT push. The tool handles all branch switching and pushing after you finish.\n\n")
	b.WriteString("Complete all steps below in order. " +
		"Do not make changes beyond what is required for the merge — resolve conflicts, validate commits and tests, and commit. Nothing else.\n\n")

	if rules != "" {
		b.WriteString("# Rules\n\n")
		b.WriteString(rules)
		b.WriteString("\n\n")
	}
	if lint != "" {
		b.WriteString("# Lint Rules\n\n")
		b.WriteString(lint)
		b.WriteString("\n\n")
	}

	b.WriteString("## Task Document\n\n")
	b.WriteString(taskContent)
	b.WriteString("\n\n")

	b.WriteString(conflictResolutionSection(conflictFiles))

	if len(conflictFiles) > 0 {
		b.WriteString("### Conflict Resolution Report\n\n")
		b.WriteString("After all conflicts are resolved and the rebase is complete, " +
			"print a summary of every conflict resolution decision you made. " +
			"For each conflicted file, explain which side you kept (ours, theirs, or a manual merge) " +
			"and why. This helps the reviewer understand what changed during the merge.\n\n")
	}

	b.WriteString("## Commit Message Validation\n\n")
	b.WriteString("Read the git log for this branch. Verify that the commit message(s) " +
		"accurately describe the changes made according to the task document above. " +
		"If any commit message is vague, misleading, or does not reflect the actual changes, " +
		"amend the most recent commit with a corrected message.\n\n")

	b.WriteString("## Test Coverage\n\n")
	b.WriteString("Verify that every feature, behavior, or change described in the task document " +
		"has corresponding test coverage. If any requirement lacks tests, add the missing tests.\n\n")

	b.WriteString(verificationSection(cmds))
	b.WriteString(commitInstructions(sign, cmds))
	b.WriteString(timeoutSection(timeout))
	if notify {
		b.WriteString(notificationSection(notifyTitle))
	}
	b.WriteString(missionReminder())

	return b.String(), nil
}

// rebaseAndPush checks out the default branch, rebases it against origin/main
// to pick up any upstream changes, then rebases against the feature branch to
// incorporate the task's commits, and pushes.
func (r *Runner) rebaseAndPush(taskRepo *repo.Repo, branch string) (string, error) {
	defaultBranch, err := r.detectDefaultBranch(taskRepo)
	if err != nil {
		return "", fmt.Errorf("detecting default branch: %w", err)
	}

	if err := taskRepo.Checkout(defaultBranch); err != nil {
		return "", fmt.Errorf("checking out %s: %w", defaultBranch, err)
	}

	// Fetch latest and rebase main against origin/main.
	if err := taskRepo.Fetch(); err != nil {
		return "", fmt.Errorf("fetching before rebase: %w", err)
	}

	originRef := "origin/" + defaultBranch
	if err := taskRepo.Rebase(originRef); err != nil {
		return "", fmt.Errorf("rebasing %s against %s: %w", defaultBranch, originRef, err)
	}

	// Rebase main against the feature branch to incorporate task commits.
	if err := taskRepo.Rebase(branch); err != nil {
		return "", fmt.Errorf("rebasing %s against %s: %w", defaultBranch, branch, err)
	}

	if err := taskRepo.PushMain(); err != nil {
		return "", fmt.Errorf("pushing main: %w", err)
	}

	return defaultBranch, nil
}

// finalizeMerge records the SHA, moves the task to completed, closes the issue,
// and deletes the remote feature branch.
func (r *Runner) finalizeMerge(task *design.Task, taskRepo *repo.Repo, taskName, branch, defaultBranch string) error {
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

	if err := taskRepo.DeleteRemoteBranch(branch); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not delete remote branch %q: %v\n", branch, err)
	}

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

// MergeGroup merges all review/merge tasks in a group sequentially.
func (r *Runner) MergeGroup(groupName string) error {
	var groupTasks []design.Task

	for _, state := range []design.TaskState{design.StateReview, design.StateMerge} {
		tasks, err := r.Design.TasksByState(state)
		if err != nil {
			return fmt.Errorf("listing %s tasks: %w", state, err)
		}
		for _, t := range tasks {
			if t.Group == groupName {
				groupTasks = append(groupTasks, t)
			}
		}
	}

	if len(groupTasks) == 0 {
		return fmt.Errorf("no review/merge tasks found in group %q", groupName)
	}

	sort.Slice(groupTasks, func(i, j int) bool {
		return groupTasks[i].Name < groupTasks[j].Name
	})

	for _, t := range groupTasks {
		taskRef := groupName + "/" + t.Name
		if err := r.Merge(taskRef); err != nil {
			return fmt.Errorf("task %s: %w", taskRef, err)
		}
	}

	return nil
}

// MergeList prints tasks in review or merge state.
func (r *Runner) MergeList() error {
	return r.listReviewMergeTasks("No tasks in review or merge state.")
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
