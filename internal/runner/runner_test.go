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

// mockClaude simulates claude by creating a file in the repo.
func mockClaude(_ context.Context, cfg ClaudeRunConfig) error {
	return os.WriteFile(filepath.Join(cfg.RepoDir, "generated.go"), []byte("package main\n"), 0o600)
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
		return os.WriteFile(filepath.Join(cfg.RepoDir, "output.txt"), []byte("done"), 0o600)
	}
}

// mockClaudeCaptureConfig captures the full ClaudeRunConfig.
func mockClaudeCaptureConfig(captured *ClaudeRunConfig) ClaudeFunc {
	return func(_ context.Context, cfg ClaudeRunConfig) error {
		*captured = cfg
		return os.WriteFile(filepath.Join(cfg.RepoDir, "output.txt"), []byte("done"), 0o600)
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
	statusOut, _ := exec.CommandContext(context.Background(), "git", "-C", wd, "status", "--porcelain").Output() //nolint:gosec // test
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
	remoteOut, _ := exec.CommandContext(context.Background(), "git", "-C", env.BareDir, "branch").Output() //nolint:gosec // test
	if !strings.Contains(string(remoteOut), "hydra/add-feature") {
		t.Errorf("branch not pushed to remote, branches: %s", remoteOut)
	}

	// Verify lock was released.
	lockPath := filepath.Join(env.BaseDir, ".hydra", "hydra.lock")
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
	out, _ := exec.CommandContext(context.Background(), "git", "-C", wd, "rev-parse", "--abbrev-ref", "HEAD").Output() //nolint:gosec // test
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
	lockPath := filepath.Join(env.BaseDir, ".hydra", "hydra.lock")
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

	// Acquire lock manually (our PID, so it's alive).
	lk := lock.New(filepath.Join(env.BaseDir, ".hydra"), "other-task")
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
		t.Fatal("expected error when lock is held")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want already running message", err)
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

	// Check the commit message.
	out, err := exec.CommandContext(context.Background(), "git", "-C", wd, "log", "-1", "--format=%s").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git log: %v", err)
	}

	msg := strings.TrimSpace(string(out))
	if msg != "hydra: add-feature" {
		t.Errorf("commit message = %q, want 'hydra: add-feature'", msg)
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
	if err := os.WriteFile(filepath.Join(hydraDir, "hydra.lock"), data, 0o600); err != nil {
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

func TestRunWithFailingTest(t *testing.T) {
	env := setupTestEnv(t)

	// Override hydra.yml with a failing test command.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"), "commands:\n  test: \"false\"\n  lint: \"true\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	err = r.Run("add-feature")
	if err == nil {
		t.Fatal("expected error when test command fails")
	}
	if !strings.Contains(err.Error(), "test step failed") {
		t.Errorf("error = %q, want test step failed message", err)
	}
}

func TestRunWithFailingLint(t *testing.T) {
	env := setupTestEnv(t)

	// Override hydra.yml with a failing lint command.
	writeFile(t, filepath.Join(env.DesignDir, "hydra.yml"), "commands:\n  test: \"true\"\n  lint: \"false\"\n")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	err = r.Run("add-feature")
	if err == nil {
		t.Fatal("expected error when lint command fails")
	}
	if !strings.Contains(err.Error(), "lint step failed") {
		t.Errorf("error = %q, want lint step failed message", err)
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
		return os.WriteFile(filepath.Join(cfg.RepoDir, fname), []byte("package main\n"), 0o600)
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
			return os.WriteFile(filepath.Join(cfg.RepoDir, fname), []byte("package main\n"), 0o600)
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

	// Second prepare should fetch + reset.
	taskRepo, err := r.prepareRepo(wd)
	if err != nil {
		t.Fatalf("second prepareRepo: %v", err)
	}
	if !repo.IsGitRepo(taskRepo.Dir) {
		t.Error("expected git repo after sync")
	}

	// Dirty file should be gone after reset.
	if _, err := os.Stat(filepath.Join(wd, "dirty.txt")); !os.IsNotExist(err) {
		t.Error("dirty file should be gone after reset")
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
		return os.WriteFile(filepath.Join(cfg.RepoDir, fname), []byte("package main\n"), 0o600)
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

	// Now run review (with mock Claude that makes changes).
	reviewCallCount := 0
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		reviewCallCount++
		return os.WriteFile(filepath.Join(cfg.RepoDir, "review-fix.go"), []byte("package main\n// fixed"), 0o600)
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

	// Merge the task.
	r.Claude = mockClaudeNoChanges // no conflicts expected
	if err := r.Merge("add-feature"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Task should be in completed state.
	dd, _ := design.NewDir(env.DesignDir)
	_, err = dd.FindTaskByState("add-feature", design.StateCompleted)
	if err != nil {
		t.Errorf("task should be completed: %v", err)
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

	r.Claude = mockClaudeNoChanges
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
