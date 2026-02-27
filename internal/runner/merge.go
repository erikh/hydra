package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
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

	// Attempt rebase onto origin/main; collect conflict info if any.
	conflictFiles, err := r.attemptRebase(taskRepo)
	if err != nil {
		return err
	}

	// Assemble single comprehensive document and invoke Claude once.
	content, _ := task.Content()
	cmds := r.commandsMap(wd)
	sign := taskRepo.HasSigningKey()
	doc := assembleMergeDocument(content, conflictFiles, cmds, sign)

	// Run before hook.
	if err := r.runBeforeHook(wd); err != nil {
		return fmt.Errorf("before hook: %w", err)
	}

	claudeFn := r.Claude
	if claudeFn == nil {
		claudeFn = invokeClaude
	}
	_ = claudeFn(context.Background(), ClaudeRunConfig{
		RepoDir:    taskRepo.Dir,
		Document:   doc,
		Model:      r.Model,
		AutoAccept: r.AutoAccept,
		PlanMode:   r.PlanMode,
	})

	// Force-push the branch (Claude may have added commits).
	if err := taskRepo.ForcePushWithLease(branch); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	// Rebase task branch into main and push.
	defaultBranch, err := r.rebaseAndPush(taskRepo, branch)
	if err != nil {
		return err
	}

	// Record SHA, complete task, close issue, clean up remote branch.
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

// attemptRebase fetches, detects the default branch, and attempts to rebase
// onto origin/<default>. If the rebase has conflicts, it aborts the rebase and
// returns the list of conflicted files. On success, returns an empty list.
func (r *Runner) attemptRebase(taskRepo *repo.Repo) ([]string, error) {
	if err := taskRepo.Fetch(); err != nil {
		return nil, fmt.Errorf("fetching: %w", err)
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
	conflictFiles, _ := taskRepo.ConflictFiles()
	_ = taskRepo.RebaseAbort()
	return conflictFiles, nil
}

// assembleMergeDocument builds a single comprehensive document for the merge
// workflow. It covers conflict resolution (if needed), test/lint verification,
// commit message validation, and test coverage — all in one Claude session.
func assembleMergeDocument(taskContent string, conflictFiles []string, cmds map[string]string, sign bool) string {
	var b strings.Builder

	b.WriteString("# Merge Workflow\n\n")
	b.WriteString("This branch is being merged into the default branch. " +
		"Complete all steps below in order. " +
		"Do not make changes beyond what is required for the merge — resolve conflicts, validate commits and tests, and commit. Nothing else.\n\n")

	b.WriteString("## Task Document\n\n")
	b.WriteString(taskContent)
	b.WriteString("\n\n")

	// Conflict resolution section (only if there are conflicts).
	if len(conflictFiles) > 0 {
		b.WriteString("## Conflict Resolution\n\n")
		b.WriteString("A rebase onto origin/main was attempted but resulted in conflicts. " +
			"The rebase has been aborted. You must:\n\n")
		b.WriteString("1. Run `git rebase origin/main`\n")
		b.WriteString("2. Resolve the conflicts in the files listed below\n")
		b.WriteString("3. Stage resolved files with `git add`\n")
		b.WriteString("4. Run `git rebase --continue`\n\n")

		b.WriteString("### Conflicted Files\n\n")
		for _, f := range conflictFiles {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
		b.WriteString("\n")
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
	b.WriteString(missionReminder())

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
