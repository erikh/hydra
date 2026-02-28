package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erikh/hydra/internal/design"
)

func TestReconcileFullWorkflow(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	// Run a task to move it to review, then complete it.
	if err := r.Run("add-feature"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	dd, _ := design.NewDir(env.DesignDir)
	task, err := dd.FindTaskByState("add-feature", design.StateReview)
	if err != nil {
		t.Fatalf("FindTaskByState: %v", err)
	}
	if err := dd.MoveTask(task, design.StateCompleted); err != nil {
		t.Fatalf("MoveTask: %v", err)
	}

	// Re-create runner to pick up fresh state.
	r, err = New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Mock Claude to update functional.md in the work dir.
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		functionalPath := filepath.Join(cfg.RepoDir, "functional.md")
		return os.WriteFile(functionalPath, []byte("# Updated Spec\n\nFeature: add-feature is implemented.\n"), 0o600)
	}

	if err := r.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify functional.md was updated in the design dir.
	content, err := os.ReadFile(filepath.Join(env.DesignDir, "functional.md"))
	if err != nil {
		t.Fatalf("reading functional.md: %v", err)
	}
	if !strings.Contains(string(content), "Updated Spec") {
		t.Errorf("functional.md not updated, got: %s", content)
	}

	// Verify completed tasks were deleted.
	completed, _ := dd.TasksByState(design.StateCompleted)
	if len(completed) != 0 {
		t.Errorf("expected 0 completed tasks, got %d", len(completed))
	}
}

func TestReconcileNoCompletedTasks(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Claude = mockClaude
	r.BaseDir = env.BaseDir

	err = r.Reconcile()
	if err == nil {
		t.Fatal("expected error when no completed tasks")
	}
	if !strings.Contains(err.Error(), "no completed tasks") {
		t.Errorf("error = %q, want no completed tasks message", err)
	}
}

func TestReconcileDocumentContents(t *testing.T) {
	env := setupTestEnv(t)

	// Set up completed tasks manually.
	mkdirAll(t, filepath.Join(env.DesignDir, "state", "completed"))
	writeFile(t, filepath.Join(env.DesignDir, "state", "completed", "task-a.md"), "Task A: add login")
	writeFile(t, filepath.Join(env.DesignDir, "state", "completed", "task-b.md"), "Task B: add signup")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Capture the document.
	var captured string
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		captured = cfg.Document
		return nil
	}

	if err := r.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify document contains completed task content.
	if !strings.Contains(captured, "Task A: add login") {
		t.Error("document missing task-a content")
	}
	if !strings.Contains(captured, "Task B: add signup") {
		t.Error("document missing task-b content")
	}

	// Verify document contains current functional.md content.
	if !strings.Contains(captured, "Tests must pass.") {
		t.Error("document missing current functional.md content")
	}

	// Verify document contains mission and instructions.
	if !strings.Contains(captured, "# Mission") {
		t.Error("document missing Mission section")
	}
	if !strings.Contains(captured, "# Instructions") {
		t.Error("document missing Instructions section")
	}
}

func TestReconcilePreservesOtherStates(t *testing.T) {
	env := setupTestEnv(t)

	// Set up completed tasks.
	mkdirAll(t, filepath.Join(env.DesignDir, "state", "completed"))
	writeFile(t, filepath.Join(env.DesignDir, "state", "completed", "done-task.md"), "Done task content")

	// Set up a review task.
	mkdirAll(t, filepath.Join(env.DesignDir, "state", "review"))
	writeFile(t, filepath.Join(env.DesignDir, "state", "review", "review-task.md"), "Review task content")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir
	r.Claude = func(_ context.Context, _ ClaudeRunConfig) error { return nil }

	if err := r.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Review task should still exist.
	dd, _ := design.NewDir(env.DesignDir)
	review, _ := dd.TasksByState(design.StateReview)
	if len(review) != 1 {
		t.Errorf("expected 1 review task, got %d", len(review))
	}

	// Pending tasks should still exist.
	pending, _ := dd.PendingTasks()
	if len(pending) == 0 {
		t.Error("pending tasks should not be affected")
	}
}

func TestReconcileClaudeFailure(t *testing.T) {
	env := setupTestEnv(t)

	// Set up completed tasks.
	mkdirAll(t, filepath.Join(env.DesignDir, "state", "completed"))
	writeFile(t, filepath.Join(env.DesignDir, "state", "completed", "done-task.md"), "Done task content")

	// Save original functional.md content.
	origFunctional, _ := os.ReadFile(filepath.Join(env.DesignDir, "functional.md"))

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir
	r.Claude = mockClaudeFailing

	err = r.Reconcile()
	if err == nil {
		t.Fatal("expected error when Claude fails")
	}

	// Completed tasks should NOT be deleted.
	dd, _ := design.NewDir(env.DesignDir)
	completed, _ := dd.TasksByState(design.StateCompleted)
	if len(completed) != 1 {
		t.Errorf("expected 1 completed task (not deleted on failure), got %d", len(completed))
	}

	// functional.md should NOT be updated.
	currentFunctional, _ := os.ReadFile(filepath.Join(env.DesignDir, "functional.md"))
	if string(currentFunctional) != string(origFunctional) {
		t.Error("functional.md should not be modified on Claude failure")
	}
}
