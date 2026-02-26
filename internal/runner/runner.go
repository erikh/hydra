// Package runner orchestrates the full hydra task lifecycle.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	branch := task.BranchName()
	if taskRepo.BranchExists(branch) {
		if err := taskRepo.Checkout(branch); err != nil {
			return fmt.Errorf("checking out branch: %w", err)
		}
	} else {
		if err := taskRepo.CreateBranch(branch); err != nil {
			return fmt.Errorf("creating branch: %w", err)
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
	if err := claudeFn(context.Background(), runCfg); err != nil {
		return err
	}

	// Verify Claude committed (HEAD moved).
	afterSHA, _ := taskRepo.LastCommitSHA()
	if afterSHA == beforeSHA {
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
