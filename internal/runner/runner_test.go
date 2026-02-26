package runner

import (
	"context"
	"encoding/json"
	"errors"
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
	writeFile(t, filepath.Join(designDir, "tasks", "backend", "add-api.md"), "Build API.")

	// Create hydra.yml with passing commands.
	writeFile(t, filepath.Join(designDir, "hydra.yml"), "commands:\n  test: \"true\"\n  lint: \"true\"\n")

	// Create state dir for record.json.
	mkdirAll(t, filepath.Join(designDir, "state"))

	// Create bare remote.
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "init", "--bare", bareDir)

	// Create repo with initial commit and remote.
	repoDir := filepath.Join(base, ".hydra", "repo")
	mkdirAll(t, repoDir)
	gitRun(t, "init", repoDir)
	gitRun(t, "-C", repoDir, "config", "user.email", "test@test.com")
	gitRun(t, "-C", repoDir, "config", "user.name", "Test")
	gitRun(t, "-C", repoDir, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# Test")
	gitRun(t, "-C", repoDir, "add", "-A")
	gitRun(t, "-C", repoDir, "commit", "-m", "initial")
	gitRun(t, "-C", repoDir, "remote", "add", "origin", bareDir)

	// Push to whatever the default branch is.
	out, err := exec.CommandContext(context.Background(), "git", "-C", repoDir, "rev-parse", "--abbrev-ref", "HEAD").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("getting branch: %v", err)
	}
	branch := strings.TrimSpace(string(out))
	gitRun(t, "-C", repoDir, "push", "-u", "origin", branch)

	// Create .hydra dir and config.
	hydraDir := filepath.Join(base, ".hydra")
	mkdirAll(t, hydraDir)

	cfg := &config.Config{
		SourceRepoURL: bareDir,
		DesignDir:     designDir,
		RepoDir:       repoDir,
	}
	if err := cfg.Save(base); err != nil {
		t.Fatal(err)
	}

	return &testEnv{
		BaseDir:   base,
		DesignDir: designDir,
		RepoDir:   repoDir,
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
func mockClaude(repoDir, _ string) error {
	return os.WriteFile(filepath.Join(repoDir, "generated.go"), []byte("package main\n"), 0o600)
}

// mockClaudeNoChanges simulates claude doing nothing.
func mockClaudeNoChanges(_, _ string) error {
	return nil
}

// mockClaudeFailing simulates claude returning an error.
func mockClaudeFailing(_, _ string) error {
	return errors.New("claude crashed")
}

// mockClaudeCapture captures the document that was passed to claude.
func mockClaudeCapture(captured *string) ClaudeFunc {
	return func(repoDir, document string) error {
		*captured = document
		return os.WriteFile(filepath.Join(repoDir, "output.txt"), []byte("done"), 0o600)
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

	// Verify branch was created.
	out, err := exec.CommandContext(context.Background(), "git", "-C", env.RepoDir, "rev-parse", "--abbrev-ref", "HEAD").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("getting branch: %v", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != "hydra/add-feature" {
		t.Errorf("branch = %q, want hydra/add-feature", branch)
	}

	// Verify the generated file was committed.
	if _, err := os.Stat(filepath.Join(env.RepoDir, "generated.go")); err != nil {
		t.Error("generated.go not found in repo")
	}

	// Verify no uncommitted changes remain.
	statusOut, _ := exec.CommandContext(context.Background(), "git", "-C", env.RepoDir, "status", "--porcelain").Output() //nolint:gosec // test
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

	out, _ := exec.CommandContext(context.Background(), "git", "-C", env.RepoDir, "rev-parse", "--abbrev-ref", "HEAD").Output() //nolint:gosec // test
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

	// Check the commit message.
	out, err := exec.CommandContext(context.Background(), "git", "-C", env.RepoDir, "log", "-1", "--format=%s").Output() //nolint:gosec // test
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

	// Checkout back to the original branch so we can create another.
	out, _ := exec.CommandContext(context.Background(), "git", "-C", env.RepoDir, "branch", "--list", "main", "master").Output() //nolint:gosec // test
	branches := strings.Fields(strings.ReplaceAll(string(out), "*", ""))
	if len(branches) > 0 {
		if err := exec.CommandContext(context.Background(), "git", "-C", env.RepoDir, "checkout", branches[0]).Run(); err != nil { //nolint:gosec // test
			t.Fatalf("checkout: %v", err)
		}
	}

	// Run second task.
	if err := r.Run("another-task"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	// Both tasks should be in review.
	dd, _ := design.NewDir(env.DesignDir)
	review, _ := dd.TasksByState(design.StateReview)
	if len(review) != 2 {
		t.Errorf("expected 2 review tasks, got %d", len(review))
	}

	// Pending should have only the grouped task left.
	pending, _ := dd.PendingTasks()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending task, got %d", len(pending))
	}
	if pending[0].Name != "add-api" {
		t.Errorf("remaining task = %q, want add-api", pending[0].Name)
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
	out, err := exec.CommandContext(context.Background(), "git", "-C", env.RepoDir, "rev-parse", "HEAD").Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	actualSHA := strings.TrimSpace(string(out))
	if entries[0]["sha"] != actualSHA {
		t.Errorf("recorded SHA = %q, actual = %q", entries[0]["sha"], actualSHA)
	}
}
