// Package runner orchestrates the full hydra task lifecycle.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/issues"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
	"github.com/erikh/hydra/internal/taskrun"
)

// ClaudeRunConfig holds the parameters for a Claude invocation.
type ClaudeRunConfig struct {
	RepoDir    string
	Document   string
	Model      string
	AutoAccept bool
	PlanMode   bool
}

// ClaudeFunc is the function signature for invoking claude.
type ClaudeFunc func(ctx context.Context, cfg ClaudeRunConfig) error

// Runner orchestrates the full hydra run workflow.
type Runner struct {
	Config      *config.Config
	Design      *design.Dir
	Claude      ClaudeFunc
	TaskRunner  *taskrun.Commands // loaded from hydra.yml; nil if not present
	BaseDir     string            // working directory for lock file; defaults to "."
	Model       string            // model name override
	AutoAccept  bool              // auto-accept all tool calls
	PlanMode    bool              // start Claude in plan mode
	Rebase      bool               // rebase onto origin/main before running
	Notify      bool              // send desktop notifications on confirmation
	IssueCloser issues.Closer     // set by merge workflow
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
		Claude:  invokeClaude,
		BaseDir: ".",
	}

	if err := r.loadHydraYml(cfg); err != nil {
		return nil, err
	}

	return r, nil
}

// loadHydraYml loads hydra.yml and resolves issue closer.
// If the file does not exist, it is created with placeholder content.
func (r *Runner) loadHydraYml(cfg *config.Config) error {
	if err := design.EnsureHydraYml(cfg.DesignDir); err != nil {
		return fmt.Errorf("ensuring hydra.yml: %w", err)
	}
	ymlPath := filepath.Join(cfg.DesignDir, "hydra.yml")

	cmds, err := taskrun.Load(ymlPath)
	if err != nil {
		return fmt.Errorf("loading hydra.yml: %w", err)
	}
	r.TaskRunner = cmds
	if cmds.Model != "" {
		r.Model = cmds.Model
	}

	r.resolveIssueCloser(cfg.SourceRepoURL, cmds.APIType, cmds.GiteaURL)
	return nil
}

// timeout returns the configured task timeout, or zero if none is set.
func (r *Runner) timeout() time.Duration {
	if r.TaskRunner != nil && r.TaskRunner.Timeout != nil {
		return r.TaskRunner.Timeout.Duration
	}
	return 0
}

// resolveIssueCloser attempts to set the issue closer from the source URL.
func (r *Runner) resolveIssueCloser(repoURL, apiType, giteaURL string) {
	source, err := issues.ResolveSource(repoURL, apiType, giteaURL)
	if err == nil {
		r.IssueCloser = issues.ResolveCloser(source)
	}
}

// commandsMap returns the effective commands from TaskRunner including
// Makefile fallbacks for the given work directory. Returns nil if TaskRunner
// is not configured.
func (r *Runner) commandsMap(workDir string) map[string]string {
	if r.TaskRunner != nil {
		return r.TaskRunner.EffectiveCommands(workDir)
	}
	return nil
}

// runBeforeHook runs the "before" command from hydra.yml if configured.
// This runs before every Claude invocation, after the repo is cloned/prepared.
func (r *Runner) runBeforeHook(workDir string) error {
	if r.TaskRunner == nil {
		return nil
	}
	return r.TaskRunner.Run("before", workDir)
}

// workDir returns the work directory path for a task.
// Ungrouped tasks: work/{name}, grouped tasks: work/{group}/{name}.
func (r *Runner) workDir(task *design.Task) string {
	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}
	if task.Group != "" {
		return filepath.Join(baseDir, "work", task.Group, task.Name)
	}
	return filepath.Join(baseDir, "work", task.Name)
}

// prepareRepo sets up the work directory for a task.
// If the directory exists and is a valid git repo, it fetches and resets.
// Otherwise, it clones fresh from the source repo URL.
func (r *Runner) prepareRepo(workDir string) (*repo.Repo, error) {
	if taskRepo, ok := r.trySyncExisting(workDir); ok {
		return taskRepo, nil
	}
	return repo.Clone(r.Config.SourceRepoURL, workDir)
}

// trySyncExisting attempts to sync an existing work directory.
// Returns the repo and true if successful, or nil and false if a fresh clone is needed.
func (r *Runner) trySyncExisting(workDir string) (*repo.Repo, bool) {
	info, err := os.Stat(workDir)
	if err != nil || !info.IsDir() {
		return nil, false
	}

	if repo.IsGitRepo(workDir) {
		if taskRepo, err := r.syncGitRepo(workDir); err == nil {
			return taskRepo, true
		}
		fmt.Fprintf(os.Stderr, "Warning: resync of %s failed, re-cloning\n", workDir)
	}

	// Not a git repo or sync failed; remove it.
	_ = os.RemoveAll(workDir)
	return nil, false
}

// syncGitRepo fetches an existing git repo without resetting the working tree.
func (r *Runner) syncGitRepo(workDir string) (*repo.Repo, error) {
	taskRepo := repo.Open(workDir)
	if err := taskRepo.Fetch(); err != nil {
		return nil, err
	}
	return taskRepo, nil
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

	// Prepare work directory
	wd := r.workDir(task)
	taskRepo, err := r.prepareRepo(wd)
	if err != nil {
		return fmt.Errorf("preparing work directory: %w", err)
	}

	// Check out existing task branch, or create a new one.
	// If the working tree is dirty, skip branch operations — let Claude work on it as-is.
	branch := task.BranchName()
	if dirty, _ := taskRepo.HasChanges(); !dirty {
		if taskRepo.BranchExists(branch) {
			if err := taskRepo.Checkout(branch); err != nil {
				return fmt.Errorf("checking out branch: %w", err)
			}
		} else {
			if err := taskRepo.CreateBranch(branch); err != nil {
				return fmt.Errorf("creating branch: %w", err)
			}
		}
	} else {
		// Dirty tree — ensure we're at least on the right branch if possible.
		if taskRepo.BranchExists(branch) {
			currentBranch, _ := taskRepo.CurrentBranch()
			if currentBranch != branch {
				fmt.Fprintf(os.Stderr, "Warning: working tree is dirty and on %s, expected %s; letting Claude continue\n", currentBranch, branch)
			}
		}
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

	// Append verification and commit instructions so Claude handles test/lint/commit.
	sign := taskRepo.HasSigningKey()
	cmds := r.commandsMap(wd)
	doc += verificationSection(cmds)
	doc += commitInstructions(sign, cmds)
	doc += timeoutSection(r.timeout())
	if r.Notify {
		doc += notificationSection()
	}
	doc += missionReminder()

	// Run before hook.
	if err := r.runBeforeHook(wd); err != nil {
		return fmt.Errorf("before hook: %w", err)
	}

	// Capture HEAD before invoking Claude.
	beforeSHA, _ := taskRepo.LastCommitSHA()

	// Invoke claude
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
		return errors.New("claude produced no changes")
	}

	// Record SHA -> task name
	record := design.NewRecord(r.Config.DesignDir)
	if err := record.Add(afterSHA, taskName); err != nil {
		return fmt.Errorf("recording SHA: %w", err)
	}

	if err := taskRepo.Push(branch); err != nil {
		return fmt.Errorf("pushing: %w", err)
	}

	// Move task to review
	if err := r.Design.MoveTask(task, design.StateReview); err != nil {
		return fmt.Errorf("moving task to review: %w", err)
	}

	fmt.Printf("Task %q completed successfully. Branch: %s\n", taskName, branch)
	return nil
}

// listReviewMergeTasks prints tasks in both review and merge states,
// sorted so that grouped tasks stay together.
func (r *Runner) listReviewMergeTasks(emptyMsg string) error {
	var all []design.Task
	for _, state := range []design.TaskState{design.StateReview, design.StateMerge} {
		tasks, err := r.Design.TasksByState(state)
		if err != nil {
			return err
		}
		all = append(all, tasks...)
	}

	if len(all) == 0 {
		fmt.Println(emptyMsg)
		return nil
	}

	// Build deduplicated label list.
	seen := make(map[string]bool)
	var labels []string
	for _, t := range all {
		label := t.Name
		if t.Group != "" {
			label = t.Group + "/" + t.Name
		}
		if !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}

	sort.Strings(labels)

	for _, label := range labels {
		fmt.Println(label)
	}
	return nil
}

// GroupList prints all unique group names from pending tasks.
func (r *Runner) GroupList() error {
	tasks, err := r.Design.PendingTasks()
	if err != nil {
		return fmt.Errorf("listing pending tasks: %w", err)
	}

	seen := make(map[string]bool)
	var groups []string
	for _, t := range tasks {
		if t.Group != "" && !seen[t.Group] {
			seen[t.Group] = true
			groups = append(groups, t.Group)
		}
	}

	if len(groups) == 0 {
		fmt.Println("No groups found.")
		return nil
	}

	sort.Strings(groups)
	for _, g := range groups {
		fmt.Println(g)
	}
	return nil
}

// GroupTasks prints all tasks in a group across all states.
func (r *Runner) GroupTasks(groupName string) error {
	tasks, err := r.Design.AllTasks()
	if err != nil {
		return fmt.Errorf("listing tasks: %w", err)
	}

	var matched []design.Task
	for _, t := range tasks {
		if t.Group == groupName {
			matched = append(matched, t)
		}
	}

	if len(matched) == 0 {
		return fmt.Errorf("no tasks found in group %q", groupName)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Name < matched[j].Name
	})

	for _, t := range matched {
		fmt.Printf("[%s] %s/%s\n", t.State, t.Group, t.Name)
	}
	return nil
}

// Sync imports open issues and cleans up completed tasks.
// It resolves the issue source from TaskRunner config, syncs issues into the
// design directory, then deletes remote branches and closes issues for
// completed/abandoned tasks.
func (r *Runner) Sync(labels []string) error {
	apiType := ""
	giteaURL := ""
	if r.TaskRunner != nil {
		apiType = r.TaskRunner.APIType
		giteaURL = r.TaskRunner.GiteaURL
	}
	source, err := issues.ResolveSource(r.Config.SourceRepoURL, apiType, giteaURL)
	if err != nil {
		return err
	}

	created, skipped, err := issues.Sync(context.Background(), r.Config.DesignDir, source, labels)
	if err != nil {
		return err
	}

	fmt.Printf("Synced issues: %d created, %d skipped\n", created, skipped)

	sourceRepo := repo.Open(r.Config.RepoDir)
	closer := issues.ResolveCloser(source)

	cleanup, err := issues.Cleanup(r.Design, sourceRepo, closer)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	if cleanup.BranchesDeleted > 0 || cleanup.IssuesClosed > 0 {
		fmt.Printf("Cleanup: %d branches deleted, %d issues closed\n",
			cleanup.BranchesDeleted, cleanup.IssuesClosed)
	}

	return nil
}

// RunGroup executes all pending tasks in a group sequentially.
// Each task gets its own cloned work directory.
func (r *Runner) RunGroup(groupName string) error {
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

	for _, t := range groupTasks {
		taskRef := groupName + "/" + t.Name
		if err := r.Run(taskRef); err != nil {
			return fmt.Errorf("task %s: %w", taskRef, err)
		}
	}

	return nil
}
