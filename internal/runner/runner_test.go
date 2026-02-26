package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
)

// testEnv sets up the full environment needed for runner tests:
// a base dir with .hydra/, a design dir with tasks, and a git repo with a remote.
type testEnv struct {
	BaseDir   string
	DesignDir string
	RepoDir   string
	BareDir   string
	Config    *config.Config
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	base := t.TempDir()
	designDir := filepath.Join(t.TempDir(), "design")
	os.MkdirAll(designDir, 0o755)

	// Create design files
	os.WriteFile(filepath.Join(designDir, "rules.md"), []byte("Follow best practices."), 0o644)
	os.WriteFile(filepath.Join(designDir, "lint.md"), []byte("Use gofmt."), 0o644)
	os.WriteFile(filepath.Join(designDir, "functional.md"), []byte("Tests must pass."), 0o644)

	os.MkdirAll(filepath.Join(designDir, "tasks"), 0o755)
	os.WriteFile(filepath.Join(designDir, "tasks", "add-feature.md"), []byte("Add the feature."), 0o644)
	os.WriteFile(filepath.Join(designDir, "tasks", "another-task.md"), []byte("Do another thing."), 0o644)

	os.MkdirAll(filepath.Join(designDir, "tasks", "backend"), 0o755)
	os.WriteFile(filepath.Join(designDir, "tasks", "backend", "add-api.md"), []byte("Build API."), 0o644)

	// Create bare remote
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	run(t, "git", "init", "--bare", bareDir)

	// Create repo with initial commit and remote
	repoDir := filepath.Join(base, ".hydra", "repo")
	os.MkdirAll(repoDir, 0o755)
	run(t, "git", "init", repoDir)
	run(t, "git", "-C", repoDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", repoDir, "config", "user.name", "Test")
	run(t, "git", "-C", repoDir, "config", "commit.gpgsign", "false")
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test"), 0o644)
	run(t, "git", "-C", repoDir, "add", "-A")
	run(t, "git", "-C", repoDir, "commit", "-m", "initial")
	run(t, "git", "-C", repoDir, "remote", "add", "origin", bareDir)
	// Push to whatever the default branch is
	out, _ := exec.Command("git", "-C", repoDir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	branch := strings.TrimSpace(string(out))
	run(t, "git", "-C", repoDir, "push", "-u", "origin", branch)

	// Create .hydra dir and config
	hydraDir := filepath.Join(base, ".hydra")
	os.MkdirAll(hydraDir, 0o755)

	cfg := &config.Config{
		SourceRepoURL: bareDir,
		DesignDir:     designDir,
		RepoDir:       repoDir,
	}
	cfg.Save(base)

	return &testEnv{
		BaseDir:   base,
		DesignDir: designDir,
		RepoDir:   repoDir,
		BareDir:   bareDir,
		Config:    cfg,
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// mockClaude simulates claude by creating a file in the repo.
func mockClaude(repoDir, document string) error {
	return os.WriteFile(filepath.Join(repoDir, "generated.go"), []byte("package main\n"), 0o644)
}

// mockClaudeNoChanges simulates claude doing nothing.
func mockClaudeNoChanges(repoDir, document string) error {
	return nil
}

// mockClaudeFailing simulates claude returning an error.
func mockClaudeFailing(repoDir, document string) error {
	return fmt.Errorf("claude crashed")
}

// mockClaudeCapture captures the document that was passed to claude.
func mockClaudeCapture(captured *string) ClaudeFunc {
	return func(repoDir, document string) error {
		*captured = document
		return os.WriteFile(filepath.Join(repoDir, "output.txt"), []byte("done"), 0o644)
	}
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

	// Verify branch was created
	out, err := exec.Command("git", "-C", env.RepoDir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("getting branch: %v", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != "hydra/add-feature" {
		t.Errorf("branch = %q, want hydra/add-feature", branch)
	}

	// Verify the generated file was committed
	if _, err := os.Stat(filepath.Join(env.RepoDir, "generated.go")); err != nil {
		t.Error("generated.go not found in repo")
	}

	// Verify no uncommitted changes remain
	statusOut, _ := exec.Command("git", "-C", env.RepoDir, "status", "--porcelain").Output()
	if strings.TrimSpace(string(statusOut)) != "" {
		t.Errorf("uncommitted changes remain: %s", statusOut)
	}

	// Verify task moved to review
	reviewPath := filepath.Join(env.DesignDir, "state", "review", "add-feature.md")
	if _, err := os.Stat(reviewPath); err != nil {
		t.Error("task not moved to state/review/")
	}

	// Verify original task file is gone
	origPath := filepath.Join(env.DesignDir, "tasks", "add-feature.md")
	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Error("original task file still exists")
	}

	// Verify push happened (branch exists on remote)
	remoteOut, _ := exec.Command("git", "-C", env.BareDir, "branch").Output()
	if !strings.Contains(string(remoteOut), "hydra/add-feature") {
		t.Errorf("branch not pushed to remote, branches: %s", remoteOut)
	}

	// Verify lock was released
	lockPath := filepath.Join(env.BaseDir, ".hydra", "hydra.lock")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file not released")
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

	out, _ := exec.Command("git", "-C", env.RepoDir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	branch := strings.TrimSpace(string(out))
	if branch != "hydra/backend/add-api" {
		t.Errorf("branch = %q, want hydra/backend/add-api", branch)
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

	// Verify document contains all sections in order
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

	// Lock should be released even on error
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

	// Acquire lock manually (our PID, so it's alive)
	lk := lock.New(filepath.Join(env.BaseDir, ".hydra"), "other-task")
	if err := lk.Acquire(); err != nil {
		t.Fatalf("manual lock Acquire: %v", err)
	}
	defer lk.Release()

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

	// Check the commit message
	out, err := exec.Command("git", "-C", env.RepoDir, "log", "-1", "--format=%s").Output()
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

	// Write a stale lock file with a dead PID
	hydraDir := filepath.Join(env.BaseDir, ".hydra")
	stalePID := 4194304
	data, _ := json.Marshal(map[string]any{"pid": stalePID, "task_name": "dead-task"})
	os.WriteFile(filepath.Join(hydraDir, "hydra.lock"), data, 0o644)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Should succeed because the lock is stale
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

	// Run first task
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Checkout back to the original branch so we can create another
	out, _ := exec.Command("git", "-C", env.RepoDir, "branch", "--list", "main", "master").Output()
	branches := strings.Fields(strings.ReplaceAll(string(out), "*", ""))
	if len(branches) > 0 {
		exec.Command("git", "-C", env.RepoDir, "checkout", branches[0]).Run()
	}

	// Run second task
	if err := r.Run("another-task"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	// Both tasks should be in review
	dd, _ := design.NewDesignDir(env.DesignDir)
	review, _ := dd.TasksByState(design.StateReview)
	if len(review) != 2 {
		t.Errorf("expected 2 review tasks, got %d", len(review))
	}

	// Pending should have only the grouped task left
	pending, _ := dd.PendingTasks()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending task, got %d", len(pending))
	}
	if pending[0].Name != "add-api" {
		t.Errorf("remaining task = %q, want add-api", pending[0].Name)
	}
}
