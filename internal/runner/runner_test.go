package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
)

// testEnv sets up the full environment needed for runner tests:
// a base dir with .hydra/, a design dir with tasks, and a bare remote.
type testEnv struct {
	BaseDir   string
	DesignDir string
	BareDir   string
	Config    *config.Config
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	base := t.TempDir()
	designDir := filepath.Join(t.TempDir(), "design")

	if err := os.MkdirAll(designDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Create design files.
	writeFile(t, filepath.Join(designDir, "rules.md"), "Follow best practices.")
	writeFile(t, filepath.Join(designDir, "lint.md"), "Use gofmt.")
	writeFile(t, filepath.Join(designDir, "functional.md"), "Tests must pass.")

	mkdirAll(t, filepath.Join(designDir, "tasks"))
	writeFile(t, filepath.Join(designDir, "tasks", "add-feature.md"), "Add the feature.")
	writeFile(t, filepath.Join(designDir, "tasks", "another-task.md"), "Do another thing.")

	mkdirAll(t, filepath.Join(designDir, "tasks", "backend"))
	writeFile(t, filepath.Join(designDir, "tasks", "backend", "group.md"), "Backend group shared context.")
	writeFile(t, filepath.Join(designDir, "tasks", "backend", "add-api.md"), "Build API.")
	writeFile(t, filepath.Join(designDir, "tasks", "backend", "add-db.md"), "Add database layer.")

	// Create hydra.yml with passing commands.
	writeFile(t, filepath.Join(designDir, "hydra.yml"), "commands:\n  test: \"true\"\n  lint: \"true\"\n")

	// Create state dir for record.json.
	mkdirAll(t, filepath.Join(designDir, "state"))

	// Create bare remote with an initial commit so clones have content.
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	setupDir := filepath.Join(t.TempDir(), "setup-repo")
	mkdirAll(t, setupDir)
	gitRun(t, "init", setupDir)
	gitRun(t, "-C", setupDir, "config", "user.email", "test@test.com")
	gitRun(t, "-C", setupDir, "config", "user.name", "Test")
	gitRun(t, "-C", setupDir, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(setupDir, "README.md"), "# Test")
	gitRun(t, "-C", setupDir, "add", "-A")
	gitRun(t, "-C", setupDir, "commit", "-m", "initial")

	// Create bare clone from the setup repo.
	gitRun(t, "clone", "--bare", setupDir, bareDir)

	// Create .hydra dir and config.
	hydraDir := filepath.Join(base, ".hydra")
	mkdirAll(t, hydraDir)

	cfg := &config.Config{
		SourceRepoURL: bareDir,
		DesignDir:     designDir,
		RepoDir:       filepath.Join(base, "repo"), // kept for compatibility but unused by runner
	}
	if err := cfg.Save(base); err != nil {
		t.Fatal(err)
	}

	return &testEnv{
		BaseDir:   base,
		DesignDir: designDir,
		BareDir:   bareDir,
		Config:    cfg,
	}
}

func gitRun(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
}

// mockCommit stages and commits all changes in the given repo dir.
func mockCommit(dir string) error {
	add := exec.CommandContext(context.Background(), "git", "add", "-A")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
	}
	commit := exec.CommandContext(context.Background(), "git", "commit", "-m", "mock commit")
	commit.Dir = dir
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}
	return nil
}

// mockClaude simulates claude by creating a file in the repo and committing.
func mockClaude(_ context.Context, cfg ClaudeRunConfig) error {
	if err := os.WriteFile(filepath.Join(cfg.RepoDir, "generated.go"), []byte("package main\n"), 0o600); err != nil {
		return err
	}
	return mockCommit(cfg.RepoDir)
}

// mockClaudeNoChanges simulates claude doing nothing.
func mockClaudeNoChanges(_ context.Context, _ ClaudeRunConfig) error {
	return nil
}

// mockClaudeFailing simulates claude returning an error.
func mockClaudeFailing(_ context.Context, _ ClaudeRunConfig) error {
	return errors.New("claude crashed")
}

// mockClaudeCapture captures the document that was passed to claude.
func mockClaudeCapture(captured *string) ClaudeFunc {
	return func(_ context.Context, cfg ClaudeRunConfig) error {
		*captured = cfg.Document
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, "output.txt"), []byte("done"), 0o600); err != nil {
			return err
		}
		return mockCommit(cfg.RepoDir)
	}
}

// mockClaudeCaptureConfig captures the full ClaudeRunConfig.
func mockClaudeCaptureConfig(captured *ClaudeRunConfig) ClaudeFunc {
	return func(_ context.Context, cfg ClaudeRunConfig) error {
		*captured = cfg
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, "output.txt"), []byte("done"), 0o600); err != nil {
			return err
		}
		return mockCommit(cfg.RepoDir)
	}
}

// workDirForTask returns the expected work directory for "add-feature" in tests.
func workDirForTask(baseDir string) string {
	return filepath.Join(baseDir, "work", "add-feature")
}

func TestRunFullWorkflow(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify work directory was created and branch exists.
	wd := workDirForTask(env.BaseDir)
	out, err := exec.CommandContext(context.Background(), "git", "-C", wd, "rev-parse", "--abbrev-ref", "HEAD").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("getting branch: %v", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != "hydra/add-feature" {
		t.Errorf("branch = %q, want hydra/add-feature", branch)
	}

	// Verify the generated file was committed.
	if _, err := os.Stat(filepath.Join(wd, "generated.go")); err != nil {
		t.Error("generated.go not found in work dir")
	}

	// Verify no uncommitted changes remain.
	statusOut, err := exec.CommandContext(context.Background(), "git", "-C", wd, "status", "--porcelain").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		t.Errorf("uncommitted changes remain: %s", statusOut)
	}

	// Verify task moved to review.
	reviewPath := filepath.Join(env.DesignDir, "state", "review", "add-feature.md")
	if _, err := os.Stat(reviewPath); err != nil {
		t.Error("task not moved to state/review/")
	}

	// Verify original task file is gone.
	origPath := filepath.Join(env.DesignDir, "tasks", "add-feature.md")
	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Error("original task file still exists")
	}

	// Verify push happened (branch exists on remote).
	remoteOut, err := exec.CommandContext(context.Background(), "git", "-C", env.BareDir, "branch").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git branch: %v", err)
	}
	if !strings.Contains(string(remoteOut), "hydra/add-feature") {
		t.Errorf("branch not pushed to remote, branches: %s", remoteOut)
	}

	// Verify lock was released.
	lockPath := filepath.Join(env.BaseDir, ".hydra", "hydra-add-feature.lock")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file not released")
	}
}

func TestRunCreatesWorkDir(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wd := workDirForTask(env.BaseDir)
	if _, err := os.Stat(wd); err != nil {
		t.Errorf("work dir not created: %v", err)
	}
	if !repo.IsGitRepo(wd) {
		t.Error("work dir is not a git repo")
	}
}

func TestRunGroupedTask(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	if err := r.Run("backend/add-api"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Grouped tasks go in work/{group}/{name}.
	wd := filepath.Join(env.BaseDir, "work", "backend", "add-api")
	out, err := exec.CommandContext(context.Background(), "git", "-C", wd, "rev-parse", "--abbrev-ref", "HEAD").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != "hydra/backend/add-api" {
		t.Errorf("branch = %q, want hydra/backend/add-api", branch)
	}
}

func TestRunGroupedTaskWorkDir(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	if err := r.Run("backend/add-api"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wd := filepath.Join(env.BaseDir, "work", "backend", "add-api")
	if _, err := os.Stat(wd); err != nil {
		t.Errorf("grouped work dir not created: %v", err)
	}
}

func TestRunDocumentAssembly(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured string
	r.Claude = mockClaudeCapture(&captured)
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify document contains all sections in order.
	if !strings.Contains(captured, "# Rules") {
		t.Error("document missing Rules section")
	}
	if !strings.Contains(captured, "Follow best practices.") {
		t.Error("document missing rules content")
	}
	if !strings.Contains(captured, "# Lint Rules") {
		t.Error("document missing Lint section")
	}
	if !strings.Contains(captured, "Use gofmt.") {
		t.Error("document missing lint content")
	}
	if !strings.Contains(captured, "# Task") {
		t.Error("document missing Task section")
	}
	if !strings.Contains(captured, "Add the feature.") {
		t.Error("document missing task content")
	}
	if !strings.Contains(captured, "# Functional Tests") {
		t.Error("document missing Functional section")
	}
	if !strings.Contains(captured, "Tests must pass.") {
		t.Error("document missing functional content")
	}
}

func TestRunNoChangesError(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaudeNoChanges
	r.BaseDir = env.BaseDir

	err = r.Run("add-feature")
	if err == nil {
		t.Fatal("expected error when claude produces no changes")
	}
	if !strings.Contains(err.Error(), "no changes") {
		t.Errorf("error = %q, want message about no changes", err)
	}

	// Lock should be released even on error.
	lockPath := filepath.Join(env.BaseDir, ".hydra", "hydra-add-feature.lock")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file not released after error")
	}
}

func TestRunClaudeError(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaudeFailing
	r.BaseDir = env.BaseDir

	err = r.Run("add-feature")
	if err == nil {
		t.Fatal("expected error when claude fails")
	}
	if !strings.Contains(err.Error(), "claude crashed") {
		t.Errorf("error = %q, want claude crashed", err)
	}
}

func TestRunClaudeErrorAfterCommit(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Simulate Claude committing then returning an error (e.g. signal termination).
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, "generated.go"), []byte("package main\n"), 0o600); err != nil {
			return err
		}
		if err := mockCommit(cfg.RepoDir); err != nil {
			return err
		}
		return errors.New("terminated by signal")
	}
	r.BaseDir = env.BaseDir

	// Run should still succeed since Claude committed.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run should succeed when Claude committed before error: %v", err)
	}

	// Task should be in review state.
	dd, _ := design.NewDir(env.DesignDir)
	_, err = dd.FindTaskByState("add-feature", design.StateReview)
	if err != nil {
		t.Error("task should be in review after Claude committed before error")
	}

	// Branch should be pushed to remote.
	remoteOut, err := exec.CommandContext(context.Background(), "git", "-C", env.BareDir, "branch").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git branch: %v", err)
	}
	if !strings.Contains(string(remoteOut), "hydra/add-feature") {
		t.Error("branch not pushed to remote after Claude committed before error")
	}
}

func TestRunTaskNotFound(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	err = r.Run("nonexistent-task")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want not found message", err)
	}
}

func TestRunLockContention(t *testing.T) {
	env := setupTestEnv(t)

	// Acquire lock for the same task (our PID, so it's alive).
	lk := lock.New(filepath.Join(env.BaseDir, ".hydra"), "add-feature")
	if err := lk.Acquire(); err != nil {
		t.Fatalf("manual lock Acquire: %v", err)
	}
	defer func() { _ = lk.Release() }()

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	err = r.Run("add-feature")
	if err == nil {
		t.Fatal("expected error when same task lock is held")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want already running message", err)
	}
}

func TestRunLockNoCrossTalkContention(t *testing.T) {
	env := setupTestEnv(t)

	// Acquire lock for a different task (our PID, so it's alive).
	lk := lock.New(filepath.Join(env.BaseDir, ".hydra"), "another-task")
	if err := lk.Acquire(); err != nil {
		t.Fatalf("manual lock Acquire: %v", err)
	}
	defer func() { _ = lk.Release() }()

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Running a different task should succeed.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run should not be blocked by a different task's lock: %v", err)
	}
}

func TestRunCommitMessage(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wd := workDirForTask(env.BaseDir)

	// Check that Claude wrote the commit message (not a generic hydra message).
	out, err := exec.CommandContext(context.Background(), "git", "-C", wd, "log", "-1", "--format=%s").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git log: %v", err)
	}

	msg := strings.TrimSpace(string(out))
	if msg == "" {
		t.Error("commit message is empty")
	}
}

func TestRunStaleLockRecovery(t *testing.T) {
	env := setupTestEnv(t)

	// Write a stale lock file with a dead PID.
	hydraDir := filepath.Join(env.BaseDir, ".hydra")
	stalePID := 4194304
	data, err := json.Marshal(map[string]any{"pid": stalePID, "task_name": "dead-task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hydraDir, "hydra-add-feature.lock"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Should succeed because the lock is stale.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run with stale lock: %v", err)
	}
}

func TestRunTaskStateAfterMultipleRuns(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run first task.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Run second task (each gets its own work dir, no branch conflicts).
	if err := r.Run("another-task"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	// Both tasks should be in review.
	dd, _ := design.NewDir(env.DesignDir)
	review, _ := dd.TasksByState(design.StateReview)
	if len(review) != 2 {
		t.Errorf("expected 2 review tasks, got %d", len(review))
	}

	// Pending should have only the grouped tasks left.
	pending, _ := dd.PendingTasks()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(pending))
	}
}

func TestRunDocumentIncludesCommitInstructions(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured string
	r.Claude = mockClaudeCapture(&captured)
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(captured, "# Commit Instructions") {
		t.Error("document missing Commit Instructions section")
	}
	if !strings.Contains(captured, "git add -A") {
		t.Error("document missing git add instruction")
	}
	if !strings.Contains(captured, "git commit") {
		t.Error("document missing git commit instruction")
	}
}

func TestRunRecordsSHA(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify record.json was created with the correct entry.
	recordPath := filepath.Join(env.DesignDir, "state", "record.json")
	data, err := os.ReadFile(recordPath) //nolint:gosec // test
	if err != nil {
		t.Fatalf("reading record.json: %v", err)
	}

	var entries []map[string]string
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parsing record.json: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 record entry, got %d", len(entries))
	}
	if entries[0]["task_name"] != "add-feature" {
		t.Errorf("task_name = %q, want add-feature", entries[0]["task_name"])
	}
	if entries[0]["sha"] == "" {
		t.Error("SHA is empty in record")
	}

	// Verify the recorded SHA matches the actual commit.
	wd := workDirForTask(env.BaseDir)
	out, err := exec.CommandContext(context.Background(), "git", "-C", wd, "rev-parse", "HEAD").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	actualSHA := strings.TrimSpace(string(out))
	if entries[0]["sha"] != actualSHA {
		t.Errorf("recorded SHA = %q, actual = %q", entries[0]["sha"], actualSHA)
	}
}

func TestRunGroupedTaskIncludesGroupContent(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured string
	r.Claude = mockClaudeCapture(&captured)
	r.BaseDir = env.BaseDir

	if err := r.Run("backend/add-api"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(captured, "# Group") {
		t.Error("document missing Group section")
	}
	if !strings.Contains(captured, "Backend group shared context.") {
		t.Error("document missing group content")
	}
	if !strings.Contains(captured, "Build API.") {
		t.Error("document missing task content")
	}
}

func TestRunGroupFullWorkflow(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Use a unique file per invocation to avoid "no changes" error.
	callCount := 0
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		callCount++
		fname := fmt.Sprintf("generated-%d.go", callCount)
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, fname), []byte("package main\n"), 0o600); err != nil {
			return err
		}
		return mockCommit(cfg.RepoDir)
	}
	r.BaseDir = env.BaseDir

	if err := r.RunGroup("backend"); err != nil {
		t.Fatalf("RunGroup: %v", err)
	}

	// Both backend tasks should be in review.
	dd, _ := design.NewDir(env.DesignDir)
	review, _ := dd.TasksByState(design.StateReview)

	reviewNames := map[string]bool{}
	for _, t := range review {
		reviewNames[t.Name] = true
	}
	if !reviewNames["add-api"] || !reviewNames["add-db"] {
		t.Errorf("expected add-api and add-db in review, got %v", review)
	}

	// No backend tasks should remain pending.
	pending, _ := dd.PendingTasks()
	for _, p := range pending {
		if p.Group == "backend" {
			t.Errorf("unexpected pending backend task: %s", p.Name)
		}
	}
}

func TestRunGroupStopsOnError(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First call succeeds, second fails.
	callCount := 0
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		callCount++
		if callCount == 1 {
			fname := fmt.Sprintf("generated-%d.go", callCount)
			if err := os.WriteFile(filepath.Join(cfg.RepoDir, fname), []byte("package main\n"), 0o600); err != nil {
				return err
			}
			return mockCommit(cfg.RepoDir)
		}
		return errors.New("claude crashed")
	}
	r.BaseDir = env.BaseDir

	err = r.RunGroup("backend")
	if err == nil {
		t.Fatal("expected error from RunGroup")
	}
	if !strings.Contains(err.Error(), "claude crashed") {
		t.Errorf("error = %q, want claude crashed", err)
	}

	// First task (add-api, alphabetically first) should be in review.
	dd, _ := design.NewDir(env.DesignDir)
	review, _ := dd.TasksByState(design.StateReview)
	if len(review) != 1 || review[0].Name != "add-api" {
		t.Errorf("review tasks = %v, want [add-api]", review)
	}

	// Second task (add-db) should still be pending.
	pending, _ := dd.PendingTasks()
	foundDB := false
	for _, p := range pending {
		if p.Group == "backend" && p.Name == "add-db" {
			foundDB = true
		}
	}
	if !foundDB {
		t.Error("add-db should still be pending")
	}
}

func TestRunGroupEmptyError(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	err = r.RunGroup("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent group")
	}
	if !strings.Contains(err.Error(), "no pending tasks") {
		t.Errorf("error = %q, want no pending tasks message", err)
	}
}

func TestPrepareRepoFreshClone(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	wd := filepath.Join(env.BaseDir, "work", "fresh-task")
	taskRepo, err := r.prepareRepo(wd)
	if err != nil {
		t.Fatalf("prepareRepo: %v", err)
	}
	if !repo.IsGitRepo(taskRepo.Dir) {
		t.Error("expected git repo after fresh clone")
	}
}

func TestPrepareRepoExistingGitDir(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	wd := filepath.Join(env.BaseDir, "work", "existing-task")

	// First clone.
	_, err = r.prepareRepo(wd)
	if err != nil {
		t.Fatalf("first prepareRepo: %v", err)
	}

	// Add a file to make it dirty.
	writeFile(t, filepath.Join(wd, "dirty.txt"), "dirty")

	// Second prepare should fetch without resetting the working tree.
	taskRepo, err := r.prepareRepo(wd)
	if err != nil {
		t.Fatalf("second prepareRepo: %v", err)
	}
	if !repo.IsGitRepo(taskRepo.Dir) {
		t.Error("expected git repo after sync")
	}

	// Dirty file should still be present (no reset/clean).
	if _, err := os.Stat(filepath.Join(wd, "dirty.txt")); os.IsNotExist(err) {
		t.Error("dirty file should be preserved after fetch-only sync")
	}
}

func TestPrepareRepoNotGitDir(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	wd := filepath.Join(env.BaseDir, "work", "not-git")
	mkdirAll(t, wd)
	writeFile(t, filepath.Join(wd, "random.txt"), "not a git repo")

	taskRepo, err := r.prepareRepo(wd)
	if err != nil {
		t.Fatalf("prepareRepo: %v", err)
	}
	if !repo.IsGitRepo(taskRepo.Dir) {
		t.Error("expected git repo after re-clone")
	}
}

func TestRunGroupNoBaseBranch(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	callCount := 0
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		callCount++
		fname := fmt.Sprintf("generated-%d.go", callCount)
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, fname), []byte("package main\n"), 0o600); err != nil {
			return err
		}
		return mockCommit(cfg.RepoDir)
	}
	r.BaseDir = env.BaseDir

	// RunGroup works without needing to track a base branch.
	if err := r.RunGroup("backend"); err != nil {
		t.Fatalf("RunGroup: %v", err)
	}

	// Each task should have its own work dir.
	apiWD := filepath.Join(env.BaseDir, "work", "backend", "add-api")
	dbWD := filepath.Join(env.BaseDir, "work", "backend", "add-db")

	if _, err := os.Stat(apiWD); err != nil {
		t.Error("add-api work dir not created")
	}
	if _, err := os.Stat(dbWD); err != nil {
		t.Error("add-db work dir not created")
	}
}

func TestFindTaskByState(t *testing.T) {
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "tasks"))
	mkdirAll(t, filepath.Join(dir, "state", "review"))
	mkdirAll(t, filepath.Join(dir, "state", "merge"))
	mkdirAll(t, filepath.Join(dir, "state", "completed"))

	writeFile(t, filepath.Join(dir, "state", "review", "task-a.md"), "Task A in review")
	writeFile(t, filepath.Join(dir, "state", "merge", "task-b.md"), "Task B in merge")
	writeFile(t, filepath.Join(dir, "state", "completed", "task-c.md"), "Task C completed")

	dd, err := design.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Find in review.
	task, err := dd.FindTaskByState("task-a", design.StateReview)
	if err != nil {
		t.Fatalf("FindTaskByState review: %v", err)
	}
	if task.Name != "task-a" {
		t.Errorf("Name = %q, want task-a", task.Name)
	}

	// Find in merge.
	task, err = dd.FindTaskByState("task-b", design.StateMerge)
	if err != nil {
		t.Fatalf("FindTaskByState merge: %v", err)
	}
	if task.Name != "task-b" {
		t.Errorf("Name = %q, want task-b", task.Name)
	}

	// Find in completed.
	task, err = dd.FindTaskByState("task-c", design.StateCompleted)
	if err != nil {
		t.Fatalf("FindTaskByState completed: %v", err)
	}
	if task.Name != "task-c" {
		t.Errorf("Name = %q, want task-c", task.Name)
	}
}

func TestFindTaskByStateNotFound(t *testing.T) {
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "tasks"))
	mkdirAll(t, filepath.Join(dir, "state", "review"))

	dd, err := design.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = dd.FindTaskByState("nonexistent", design.StateReview)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want not found", err)
	}
}

func TestReviewWorkflow(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// First run the task to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Now run review (with mock Claude that makes changes and commits).
	reviewCallCount := 0
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		reviewCallCount++
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, "review-fix.go"), []byte("package main\n// fixed"), 0o600); err != nil {
			return err
		}
		return mockCommit(cfg.RepoDir)
	}

	if err := r.Review("add-feature"); err != nil {
		t.Fatalf("Review: %v", err)
	}

	if reviewCallCount != 1 {
		t.Errorf("expected 1 review call, got %d", reviewCallCount)
	}

	// Task should still be in review.
	dd, _ := design.NewDir(env.DesignDir)
	task, err := dd.FindTaskByState("add-feature", design.StateReview)
	if err != nil {
		t.Errorf("task should still be in review: %v", err)
	}
	if task == nil {
		t.Error("task not found in review state")
	}
}

func TestReviewNoChanges(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Review with no changes.
	r.Claude = mockClaudeNoChanges

	if err := r.Review("add-feature"); err != nil {
		t.Fatalf("Review: %v", err)
	}

	// Task should still be in review.
	dd, _ := design.NewDir(env.DesignDir)
	_, err = dd.FindTaskByState("add-feature", design.StateReview)
	if err != nil {
		t.Error("task should still be in review after no-change review")
	}
}

func TestMergeWorkflow(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Track Claude invocations during merge.
	mergeCallCount := 0
	r.Claude = func(_ context.Context, _ ClaudeRunConfig) error {
		mergeCallCount++
		return nil
	}

	if err := r.Merge("add-feature"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Merge should invoke Claude exactly once.
	if mergeCallCount != 1 {
		t.Errorf("expected 1 Claude invocation during merge, got %d", mergeCallCount)
	}

	// Task should be in completed state.
	dd, _ := design.NewDir(env.DesignDir)
	_, err = dd.FindTaskByState("add-feature", design.StateCompleted)
	if err != nil {
		t.Errorf("task should be completed: %v", err)
	}
}

func TestMergeUsesRebase(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run task to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Merge the task.
	r.Claude = func(_ context.Context, _ ClaudeRunConfig) error { return nil }
	if err := r.Merge("add-feature"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Verify main was updated via rebase (no merge commits).
	wd := workDirForTask(env.BaseDir)
	out, err := exec.CommandContext(context.Background(), "git", "-C", wd, "log", "--oneline", "--merges", "main").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("expected no merge commits on main, got: %s", out)
	}

	// Verify the task branch commit is an ancestor of main.
	err = exec.CommandContext(context.Background(), "git", "-C", wd, "merge-base", "--is-ancestor", "hydra/add-feature", "main").Run() //nolint:gosec // test
	if err != nil {
		t.Error("task branch should be an ancestor of main after rebase")
	}
}

func TestMergeFromReviewState(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	r.Claude = func(_ context.Context, _ ClaudeRunConfig) error { return nil }
	if err := r.Merge("add-feature"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	dd, _ := design.NewDir(env.DesignDir)
	_, err = dd.FindTaskByState("add-feature", design.StateCompleted)
	if err != nil {
		t.Errorf("task should be completed after merge: %v", err)
	}
}

func TestMergeCRUDOperatesOnMergeState(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Move a task to merge state manually.
	dd, _ := design.NewDir(env.DesignDir)
	task, err := dd.FindTask("add-feature")
	if err != nil {
		t.Fatal(err)
	}
	if err := dd.MoveTask(task, design.StateMerge); err != nil {
		t.Fatal(err)
	}

	// MergeView should find it.
	if err := r.MergeView("add-feature"); err != nil {
		t.Errorf("MergeView: %v", err)
	}

	// MergeRemove should move to abandoned.
	if err := r.MergeRemove("add-feature"); err != nil {
		t.Errorf("MergeRemove: %v", err)
	}

	_, err = dd.FindTaskByState("add-feature", design.StateAbandoned)
	if err != nil {
		t.Error("task should be abandoned after MergeRemove")
	}
}

func TestRunWithModelOverride(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured ClaudeRunConfig
	r.Claude = mockClaudeCaptureConfig(&captured)
	r.BaseDir = env.BaseDir
	r.Model = "claude-haiku-4-5-20251001"

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if captured.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("Model = %q, want claude-haiku-4-5-20251001", captured.Model)
	}
}

func TestCommitInstructionsUnsigned(t *testing.T) {
	result := commitInstructions(false, map[string]string{
		"test": "go test ./...",
		"lint": "golangci-lint run",
	})

	if !strings.Contains(result, "# Commit Instructions") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "go test ./...") {
		t.Error("missing test command")
	}
	if !strings.Contains(result, "golangci-lint run") {
		t.Error("missing lint command")
	}
	if !strings.Contains(result, "git add -A") {
		t.Error("missing git add instruction")
	}
	if !strings.Contains(result, "git commit -m") {
		t.Error("missing git commit instruction")
	}
	if strings.Contains(result, "-S") {
		t.Error("should not contain -S for unsigned commits")
	}
	if !strings.Contains(result, "Co-Authored-By") {
		t.Error("missing no-trailers instruction")
	}
}

func TestCommitInstructionsExclusiveCommands(t *testing.T) {
	result := commitInstructions(false, map[string]string{
		"test": "go test ./...",
		"lint": "golangci-lint run",
	})

	if !strings.Contains(result, "Do NOT run any individual test") {
		t.Error("missing individual test prohibition in commit instructions")
	}
}

func TestVerificationSectionExclusiveCommands(t *testing.T) {
	result := verificationSection(map[string]string{
		"test": "go test ./...",
		"lint": "golangci-lint run",
	})

	if !strings.Contains(result, "Do not run other commands") {
		t.Error("missing exclusive commands directive in verification section")
	}
	if !strings.Contains(result, "listed below") {
		t.Error("directive should reference commands listed below")
	}
}

func TestMergeDocumentExclusiveCommands(t *testing.T) {
	cmds := map[string]string{
		"test": "go test ./...",
		"lint": "golangci-lint run",
	}
	result := assembleMergeDocument("Task content", nil, cmds, false, 0, false)

	if !strings.Contains(result, "Do NOT run any individual test") {
		t.Error("missing individual test prohibition in merge document")
	}
}

func TestTestDocumentDoesNotContainTestLintCommands(t *testing.T) {
	result := assembleTestDocument("Task content")

	if strings.Contains(result, "go test") {
		t.Error("test document should not contain inline test commands")
	}
	if strings.Contains(result, "golangci-lint") {
		t.Error("test document should not contain inline lint commands")
	}
}

func TestRunDocumentExclusiveCommands(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured string
	r.Claude = mockClaudeCapture(&captured)
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(captured, "Do NOT run any individual test") {
		t.Error("run document missing individual test prohibition")
	}
}

func TestReviewDocumentExclusiveCommands(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var captured string
	r.Claude = mockClaudeCapture(&captured)

	if err := r.Review("add-feature"); err != nil {
		t.Fatalf("Review: %v", err)
	}

	if !strings.Contains(captured, "Do NOT run any individual test") {
		t.Error("review document missing individual test prohibition")
	}
}

func TestCommitInstructionsSigned(t *testing.T) {
	result := commitInstructions(true, nil)

	if !strings.Contains(result, "git commit -S") {
		t.Error("should contain -S for signed commits")
	}
}

func TestCommitInstructionsNilCommands(t *testing.T) {
	result := commitInstructions(false, nil)

	if strings.Contains(result, "Run the test suite") {
		t.Error("should not mention test suite when commands is nil")
	}
	if strings.Contains(result, "Run the linter") {
		t.Error("should not mention linter when commands is nil")
	}
	if !strings.Contains(result, "git commit -m") {
		t.Error("should still contain commit instruction")
	}
}

func TestVerificationSectionWithCommands(t *testing.T) {
	result := verificationSection(map[string]string{
		"test": "go test ./...",
		"lint": "golangci-lint run",
	})

	if !strings.Contains(result, "## Verification") {
		t.Error("missing Verification header")
	}
	if !strings.Contains(result, "go test ./...") {
		t.Error("missing test command")
	}
	if !strings.Contains(result, "golangci-lint run") {
		t.Error("missing lint command")
	}
	if !strings.Contains(result, "concurrently") {
		t.Error("missing concurrency safety note")
	}
}

func TestVerificationSectionNilCommands(t *testing.T) {
	result := verificationSection(nil)
	if result != "" {
		t.Errorf("expected empty string for nil commands, got %q", result)
	}
}

func TestVerificationSectionEmptyCommands(t *testing.T) {
	result := verificationSection(map[string]string{})
	if result != "" {
		t.Errorf("expected empty string for empty commands, got %q", result)
	}
}

func TestRunDocumentIncludesVerification(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured string
	r.Claude = mockClaudeCapture(&captured)
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(captured, "## Verification") {
		t.Error("run document missing Verification section")
	}
	// Our test hydra.yml has test: "true" and lint: "true".
	if !strings.Contains(captured, "Run tests:") {
		t.Error("run document missing test command in verification")
	}
	if !strings.Contains(captured, "Run linter:") {
		t.Error("run document missing lint command in verification")
	}
}

func TestReviewDocumentIncludesVerification(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var captured string
	r.Claude = mockClaudeCapture(&captured)

	if err := r.Review("add-feature"); err != nil {
		t.Fatalf("Review: %v", err)
	}

	if !strings.Contains(captured, "## Verification") {
		t.Error("review document missing Verification section")
	}
}

func TestReviewDocumentIncludesValidation(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Capture the review document.
	var captured string
	r.Claude = mockClaudeCapture(&captured)

	if err := r.Review("add-feature"); err != nil {
		t.Fatalf("Review: %v", err)
	}

	// Verify commit message validation section.
	if !strings.Contains(captured, "Commit Message Validation") {
		t.Error("review document missing commit message validation section")
	}
	if !strings.Contains(captured, "git log") {
		t.Error("review document should instruct to read git log")
	}

	// Verify test coverage validation section.
	if !strings.Contains(captured, "Test Coverage Validation") {
		t.Error("review document missing test coverage validation section")
	}
	if !strings.Contains(captured, "test coverage") {
		t.Error("review document should mention test coverage")
	}

	// Verify commit instructions are appended.
	if !strings.Contains(captured, "# Commit Instructions") {
		t.Error("review document missing commit instructions")
	}
}

func TestReviewCommitsAndPushes(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Review with Claude that makes changes and commits.
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, "review-fix.go"), []byte("package main\n// review fix"), 0o600); err != nil {
			return err
		}
		return mockCommit(cfg.RepoDir)
	}

	if err := r.Review("add-feature"); err != nil {
		t.Fatalf("Review: %v", err)
	}

	// Verify the review commit was pushed to the remote.
	wd := workDirForTask(env.BaseDir)
	localSHA, err := exec.CommandContext(context.Background(), "git", "-C", wd, "rev-parse", "HEAD").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}

	remoteSHA, err := exec.CommandContext(context.Background(), "git", "-C", env.BareDir, "rev-parse", "hydra/add-feature").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git rev-parse remote: %v", err)
	}

	if strings.TrimSpace(string(localSHA)) != strings.TrimSpace(string(remoteSHA)) {
		t.Errorf("local SHA %q != remote SHA %q", strings.TrimSpace(string(localSHA)), strings.TrimSpace(string(remoteSHA)))
	}

	// Verify record.json has the review entry.
	recordPath := filepath.Join(env.DesignDir, "state", "record.json")
	data, err := os.ReadFile(recordPath) //nolint:gosec // test
	if err != nil {
		t.Fatalf("reading record.json: %v", err)
	}
	var entries []map[string]string
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parsing record.json: %v", err)
	}
	foundReview := false
	for _, e := range entries {
		if e["task_name"] == "review:add-feature" {
			foundReview = true
		}
	}
	if !foundReview {
		t.Error("record.json missing review:add-feature entry")
	}
}

func TestMergeDocumentContents(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task to move to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Capture the single document passed to Claude during merge.
	var captured string
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		captured = cfg.Document
		return nil
	}

	if err := r.Merge("add-feature"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Verify the single document contains all expected sections.
	for _, want := range []string{
		"Merge Workflow",
		"Task Document",
		"Commit Message Validation",
		"Test Coverage",
		"Commit Instructions",
		"Add the feature.",
	} {
		if !strings.Contains(captured, want) {
			t.Errorf("merge document missing %q", want)
		}
	}

	// No conflicts expected, so no conflict section.
	if strings.Contains(captured, "Conflict Resolution") {
		t.Error("merge document should not contain Conflict Resolution when no conflicts exist")
	}
}

func TestAutoCreateHydraYml(t *testing.T) {
	env := setupTestEnv(t)

	// Remove the hydra.yml that setupTestEnv created.
	ymlPath := filepath.Join(env.DesignDir, "hydra.yml")
	if err := os.Remove(ymlPath); err != nil {
		t.Fatalf("removing hydra.yml: %v", err)
	}

	// Creating a new runner should auto-create hydra.yml.
	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Verify the file was created.
	if _, err := os.Stat(ymlPath); err != nil {
		t.Fatalf("hydra.yml not auto-created: %v", err)
	}

	// Verify the content matches the default placeholder.
	data, err := os.ReadFile(ymlPath) //nolint:gosec // test
	if err != nil {
		t.Fatalf("reading hydra.yml: %v", err)
	}
	if string(data) != design.DefaultHydraYml {
		t.Errorf("hydra.yml content = %q, want %q", string(data), design.DefaultHydraYml)
	}

	// Verify runner still works (commands are commented out so TaskRunner has no active commands).
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run after auto-create: %v", err)
	}
}

func TestTestWorkflow(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Run test session: Claude adds a test file and commits.
	testFiles := []string{"feature_test.go", "feature_helper_test.go"}
	callCount := 0
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		callCount++
		for _, f := range testFiles {
			if err := os.WriteFile(filepath.Join(cfg.RepoDir, f), []byte("package main\n// "+f), 0o600); err != nil {
				return err
			}
		}
		return mockCommit(cfg.RepoDir)
	}

	if err := r.Test("add-feature"); err != nil {
		t.Fatalf("Test: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 test call, got %d", callCount)
	}

	// Task should still be in review state.
	dd, _ := design.NewDir(env.DesignDir)
	task, err := dd.FindTaskByState("add-feature", design.StateReview)
	if err != nil {
		t.Errorf("expected task in review state: %v", err)
	}
	if task == nil {
		t.Fatal("task not found in review state")
	}

	// Verify the test files were actually written.
	wd := workDirForTask(env.BaseDir)
	for _, f := range testFiles {
		if _, statErr := os.Stat(filepath.Join(wd, f)); statErr != nil {
			t.Errorf("expected test file %s to exist: %v", f, statErr)
		}
	}
}

func TestTestNoChanges(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Test session with no changes.
	r.Claude = mockClaudeNoChanges

	if err := r.Test("add-feature"); err != nil {
		t.Fatalf("Test: %v", err)
	}

	// Task should still be in review.
	dd, _ := design.NewDir(env.DesignDir)
	_, err = dd.FindTaskByState("add-feature", design.StateReview)
	if err != nil {
		t.Error("task should still be in review after no-change test session")
	}
}

func TestTestDocumentContents(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Capture the test document.
	var captured string
	r.Claude = mockClaudeCapture(&captured)

	if err := r.Test("add-feature"); err != nil {
		t.Fatalf("Test: %v", err)
	}

	// Verify document contains test instructions.
	if !strings.Contains(captured, "# Test Instructions") {
		t.Error("document missing Test Instructions section")
	}
	if !strings.Contains(captured, "Add the feature.") {
		t.Error("document missing task content")
	}
	if !strings.Contains(captured, "test coverage") {
		t.Error("document should mention test coverage")
	}
	if !strings.Contains(captured, "# Commit Instructions") {
		t.Error("document missing commit instructions")
	}
}

func TestTestPushesChanges(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task first.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Test session with changes.
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, "new_test.go"), []byte("package main\n// test"), 0o600); err != nil {
			return err
		}
		return mockCommit(cfg.RepoDir)
	}

	if err := r.Test("add-feature"); err != nil {
		t.Fatalf("Test: %v", err)
	}

	// Verify pushed to remote.
	wd := workDirForTask(env.BaseDir)
	localSHA, err := exec.CommandContext(context.Background(), "git", "-C", wd, "rev-parse", "HEAD").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git rev-parse local: %v", err)
	}
	remoteSHA, err := exec.CommandContext(context.Background(), "git", "-C", env.BareDir, "rev-parse", "hydra/add-feature").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git rev-parse remote: %v", err)
	}

	if strings.TrimSpace(string(localSHA)) != strings.TrimSpace(string(remoteSHA)) {
		t.Error("test changes not pushed to remote")
	}

	// Verify record.json has the test entry.
	recordPath := filepath.Join(env.DesignDir, "state", "record.json")
	data, err := os.ReadFile(recordPath) //nolint:gosec // test
	if err != nil {
		t.Fatalf("reading record.json: %v", err)
	}
	var entries []map[string]string
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parsing record.json: %v", err)
	}
	foundTest := false
	for _, e := range entries {
		if e["task_name"] == "test:add-feature" {
			foundTest = true
		}
	}
	if !foundTest {
		t.Error("record.json missing test:add-feature entry")
	}
}

func TestReviewDevRunsCommand(t *testing.T) {
	env := setupTestEnv(t)

	// Add dev command to hydra.yml.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  dev: \"echo dev-running\"\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Re-create runner to pick up updated hydra.yml.
	r, err = New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// ReviewDev should succeed and run the command.
	if err := r.ReviewDev(context.Background(), "add-feature"); err != nil {
		t.Fatalf("ReviewDev: %v", err)
	}
}

func TestReviewDevMissingCommand(t *testing.T) {
	env := setupTestEnv(t)

	// hydra.yml without dev command.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// ReviewDev should fail because no dev command is configured.
	err = r.ReviewDev(context.Background(), "add-feature")
	if err == nil {
		t.Fatal("expected error when dev command is not configured")
	}
	if !strings.Contains(err.Error(), "no dev command") && !strings.Contains(err.Error(), "no dev") {
		t.Errorf("error = %q, want message about no dev command", err)
	}
}

func TestCleanRunsCommand(t *testing.T) {
	env := setupTestEnv(t)

	// Add clean command to hydra.yml.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  clean: \"touch cleaned.txt\"\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task to move it to review and create a work dir.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Re-create runner to pick up updated hydra.yml.
	r, err = New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	if err := r.Clean("add-feature"); err != nil {
		t.Fatalf("Clean: %v", err)
	}

	// Verify the clean command ran in the correct work dir.
	wd := workDirForTask(env.BaseDir)
	if _, err := os.Stat(filepath.Join(wd, "cleaned.txt")); err != nil {
		t.Error("clean command did not run in the work directory")
	}
}

func TestCleanMissingCommand(t *testing.T) {
	env := setupTestEnv(t)

	// hydra.yml without clean command.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Clean should fail because no clean command is configured (and no Makefile).
	err = r.Clean("add-feature")
	if err == nil {
		t.Fatal("expected error when clean command is not configured")
	}
	if !strings.Contains(err.Error(), "no clean command") {
		t.Errorf("error = %q, want message about no clean command", err)
	}
}

func TestCleanFallsBackToMakefile(t *testing.T) {
	env := setupTestEnv(t)

	// hydra.yml without clean command.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task to create the work directory.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Write a Makefile with a clean target in the work dir.
	wd := workDirForTask(env.BaseDir)
	writeFile(t, filepath.Join(wd, "Makefile"), "clean:\n\ttouch makefile-cleaned.txt\n")

	// Re-create runner.
	r, err = New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Clean should fall back to make clean.
	if err := r.Clean("add-feature"); err != nil {
		t.Fatalf("Clean with Makefile fallback: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wd, "makefile-cleaned.txt")); err != nil {
		t.Error("make clean did not run in the work directory")
	}
}

func TestCleanFindsTaskInAnyState(t *testing.T) {
	env := setupTestEnv(t)

	// Add clean command to hydra.yml.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  clean: \"touch cleaned.txt\"\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run add-feature to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Move add-feature from review to completed.
	dd, _ := design.NewDir(env.DesignDir)
	task, err := dd.FindTaskByState("add-feature", design.StateReview)
	if err != nil {
		t.Fatalf("FindTaskByState: %v", err)
	}
	if err := dd.MoveTask(task, design.StateCompleted); err != nil {
		t.Fatalf("MoveTask: %v", err)
	}

	// Re-create runner to pick up updated hydra.yml.
	r, err = New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Clean should still find the task in completed state.
	if err := r.Clean("add-feature"); err != nil {
		t.Fatalf("Clean on completed task: %v", err)
	}

	wd := workDirForTask(env.BaseDir)
	if _, err := os.Stat(filepath.Join(wd, "cleaned.txt")); err != nil {
		t.Error("clean command did not run for completed task")
	}
}

func TestFindTaskAny(t *testing.T) {
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "tasks"))
	mkdirAll(t, filepath.Join(dir, "state", "review"))
	mkdirAll(t, filepath.Join(dir, "state", "merge"))
	mkdirAll(t, filepath.Join(dir, "state", "completed"))
	mkdirAll(t, filepath.Join(dir, "state", "abandoned"))

	writeFile(t, filepath.Join(dir, "tasks", "pending-task.md"), "Pending task")
	writeFile(t, filepath.Join(dir, "state", "review", "review-task.md"), "Review task")
	writeFile(t, filepath.Join(dir, "state", "completed", "done-task.md"), "Done task")

	dd, err := design.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Find pending task.
	task, err := dd.FindTaskAny("pending-task")
	if err != nil {
		t.Fatalf("FindTaskAny pending: %v", err)
	}
	if task.Name != "pending-task" || task.State != design.StatePending {
		t.Errorf("got %q state=%s, want pending-task pending", task.Name, task.State)
	}

	// Find review task.
	task, err = dd.FindTaskAny("review-task")
	if err != nil {
		t.Fatalf("FindTaskAny review: %v", err)
	}
	if task.Name != "review-task" || task.State != design.StateReview {
		t.Errorf("got %q state=%s, want review-task review", task.Name, task.State)
	}

	// Find completed task.
	task, err = dd.FindTaskAny("done-task")
	if err != nil {
		t.Fatalf("FindTaskAny completed: %v", err)
	}
	if task.Name != "done-task" || task.State != design.StateCompleted {
		t.Errorf("got %q state=%s, want done-task completed", task.Name, task.State)
	}

	// Not found.
	_, err = dd.FindTaskAny("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want not found", err)
	}
}

func TestReviewDevContextCancellation(t *testing.T) {
	env := setupTestEnv(t)

	// Use a long-running dev command (sleep).
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  dev: \"sleep 60\"\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run the task to move it to review.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Re-create runner.
	r, err = New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Cancel context immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should return nil (friendly message printed instead of error).
	if err = r.ReviewDev(ctx, "add-feature"); err != nil {
		t.Fatalf("ReviewDev should return nil on cancellation, got: %v", err)
	}
}

func TestMergeDocumentWithConflicts(t *testing.T) {
	cmds := map[string]string{
		"test": "go test ./...",
		"lint": "golangci-lint run",
	}
	conflictFiles := []string{"main.go", "config.go"}
	result := assembleMergeDocument("Task content", conflictFiles, cmds, false, 0, false)

	// Verify conflict section is present.
	if !strings.Contains(result, "Conflict Resolution") {
		t.Error("merge document missing Conflict Resolution section")
	}
	if !strings.Contains(result, "main.go") {
		t.Error("merge document missing conflict file main.go")
	}
	if !strings.Contains(result, "config.go") {
		t.Error("merge document missing conflict file config.go")
	}
	if !strings.Contains(result, "git rebase origin/main") {
		t.Error("merge document missing rebase instruction")
	}
	if !strings.Contains(result, "git rebase --continue") {
		t.Error("merge document missing rebase --continue instruction")
	}

	// Verify other sections are still present.
	for _, want := range []string{
		"Task Document",
		"Commit Message Validation",
		"Test Coverage",
		"Commit Instructions",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("merge document with conflicts missing %q", want)
		}
	}
}

func TestBeforeHookRunsBeforeClaude(t *testing.T) {
	env := setupTestEnv(t)

	// Configure a before command that creates a marker file.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  before: \"touch before-ran.txt\"\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Verify the before command ran by checking for the marker file.
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		markerPath := filepath.Join(cfg.RepoDir, "before-ran.txt")
		if _, statErr := os.Stat(markerPath); statErr != nil {
			t.Error("before hook did not run before Claude invocation")
		}
		if err := os.WriteFile(filepath.Join(cfg.RepoDir, "generated.go"), []byte("package main\n"), 0o600); err != nil {
			return err
		}
		return mockCommit(cfg.RepoDir)
	}
	r.BaseDir = env.BaseDir

	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestBeforeHookFailureAborts(t *testing.T) {
	env := setupTestEnv(t)

	// Configure a before command that fails.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  before: \"false\"\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	err = r.Run("add-feature")
	if err == nil {
		t.Fatal("expected error when before hook fails")
	}
	if !strings.Contains(err.Error(), "before hook") {
		t.Errorf("error = %q, want before hook message", err)
	}
}

func TestNoBeforeHookSkipsSilently(t *testing.T) {
	env := setupTestEnv(t)

	// No before command configured.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"),
		"commands:\n  test: \"true\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Should succeed without a before command.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestDocumentsProhibitIndividualTestLint(t *testing.T) {
	// commitInstructions must always prohibit manual test/lint runs,
	// even when no commands are configured.
	ci := commitInstructions(false, nil)
	if !strings.Contains(ci, "Do NOT run any individual test") {
		t.Error("commitInstructions missing individual test prohibition when no commands configured")
	}

	ci = commitInstructions(false, map[string]string{
		"test": "go test ./...",
		"lint": "golangci-lint run",
	})
	if !strings.Contains(ci, "Do NOT run any individual test") {
		t.Error("commitInstructions missing individual test prohibition when commands configured")
	}
}
