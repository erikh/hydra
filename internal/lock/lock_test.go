package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()

	lk := New(dir, "test-task")
	must(t, lk.Acquire())

	// Lock file should exist with the per-task filename.
	lockPath := filepath.Join(dir, lockFileName("test-task"))
	data, err := os.ReadFile(lockPath) //nolint:gosec // test reads from temp dir
	if err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	var ld lockData
	if err := json.Unmarshal(data, &ld); err != nil {
		t.Fatalf("lock file invalid JSON: %v", err)
	}

	if ld.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", ld.PID, os.Getpid())
	}
	if ld.TaskName != "test-task" {
		t.Errorf("TaskName = %q, want test-task", ld.TaskName)
	}

	must(t, lk.Release())

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file still exists after Release")
	}
}

func TestAcquireBlockedBySameTask(t *testing.T) {
	dir := t.TempDir()

	// First lock (our own PID, so it's alive).
	lk1 := New(dir, "task-1")
	must(t, lk1.Acquire())

	// Second lock with the same task name should fail.
	lk2 := New(dir, "task-1")
	err := lk2.Acquire()
	if err == nil {
		t.Fatal("expected error when same task lock is held by live process")
	}

	must(t, lk1.Release())
}

func TestAcquireNotBlockedByDifferentTask(t *testing.T) {
	dir := t.TempDir()

	lk1 := New(dir, "task-1")
	must(t, lk1.Acquire())

	// Different task name should succeed.
	lk2 := New(dir, "task-2")
	if err := lk2.Acquire(); err != nil {
		t.Fatalf("different task should not be blocked: %v", err)
	}

	must(t, lk1.Release())
	must(t, lk2.Release())
}

func TestAcquireStaleLock(t *testing.T) {
	dir := t.TempDir()

	// Write a lock with a PID that definitely doesn't exist.
	stalePID := 4194304
	data, err := json.Marshal(&lockData{PID: stalePID, TaskName: "stale-task"})
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(dir, lockFileName("stale-task")), data, 0o600))

	lk := New(dir, "stale-task")
	must(t, lk.Acquire())

	// Verify we now hold the lock.
	readData, err := os.ReadFile(filepath.Join(dir, lockFileName("stale-task"))) //nolint:gosec // test reads from temp dir
	if err != nil {
		t.Fatal(err)
	}

	var ld lockData
	if err := json.Unmarshal(readData, &ld); err != nil {
		t.Fatal(err)
	}

	if ld.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", ld.PID, os.Getpid())
	}
	if ld.TaskName != "stale-task" {
		t.Errorf("TaskName = %q, want stale-task", ld.TaskName)
	}

	must(t, lk.Release())
}

func TestReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()

	lk := New(dir, "test-task")
	// Release without Acquire should not error.
	must(t, lk.Release())

	// Double release.
	must(t, lk.Acquire())
	must(t, lk.Release())
	must(t, lk.Release())
}

func TestReadAll(t *testing.T) {
	dir := t.TempDir()

	lk1 := New(dir, "running-task-1")
	must(t, lk1.Acquire())
	defer func() { must(t, lk1.Release()) }()

	lk2 := New(dir, "running-task-2")
	must(t, lk2.Acquire())
	defer func() { must(t, lk2.Release()) }()

	tasks, err := ReadAll(dir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 running tasks, got %d", len(tasks))
	}

	names := map[string]bool{}
	for _, rt := range tasks {
		names[rt.TaskName] = true
		if rt.PID != os.Getpid() {
			t.Errorf("PID = %d, want %d", rt.PID, os.Getpid())
		}
	}
	if !names["running-task-1"] || !names["running-task-2"] {
		t.Errorf("expected running-task-1 and running-task-2, got %v", names)
	}
}

func TestReadAllNoLocks(t *testing.T) {
	dir := t.TempDir()

	tasks, err := ReadAll(dir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 running tasks, got %d", len(tasks))
	}
}

func TestReadAllStaleLock(t *testing.T) {
	dir := t.TempDir()

	stalePID := 4194304
	data, err := json.Marshal(&lockData{PID: stalePID, TaskName: "stale"})
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(dir, lockFileName("stale")), data, 0o600))

	tasks, err := ReadAll(dir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 running tasks (stale should be filtered), got %d", len(tasks))
	}
}

func TestLockFileNameGroupedTask(t *testing.T) {
	name := lockFileName("backend/add-api")
	if name != "hydra-backend--add-api.lock" {
		t.Errorf("lockFileName = %q, want hydra-backend--add-api.lock", name)
	}
}
