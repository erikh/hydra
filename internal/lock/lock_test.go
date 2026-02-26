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

	// Lock file should exist.
	lockPath := filepath.Join(dir, LockFile)
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

func TestAcquireBlockedByLiveLock(t *testing.T) {
	dir := t.TempDir()

	// First lock (our own PID, so it's alive).
	lk1 := New(dir, "task-1")
	must(t, lk1.Acquire())

	// Second lock should fail.
	lk2 := New(dir, "task-2")
	err := lk2.Acquire()
	if err == nil {
		t.Fatal("expected error when lock is held by live process")
	}

	must(t, lk1.Release())
}

func TestAcquireStaleLock(t *testing.T) {
	dir := t.TempDir()

	// Write a lock with a PID that definitely doesn't exist.
	stalePID := 4194304
	data, err := json.Marshal(&lockData{PID: stalePID, TaskName: "stale-task"})
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(dir, LockFile), data, 0o600))

	lk := New(dir, "new-task")
	must(t, lk.Acquire())

	// Verify we now hold the lock.
	readData, err := os.ReadFile(filepath.Join(dir, LockFile)) //nolint:gosec // test reads from temp dir
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
	if ld.TaskName != "new-task" {
		t.Errorf("TaskName = %q, want new-task", ld.TaskName)
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

func TestReadCurrent(t *testing.T) {
	dir := t.TempDir()

	lk := New(dir, "running-task")
	must(t, lk.Acquire())
	defer func() { must(t, lk.Release()) }()

	taskName, pid, err := ReadCurrent(dir)
	if err != nil {
		t.Fatalf("ReadCurrent: %v", err)
	}

	if taskName != "running-task" {
		t.Errorf("taskName = %q, want running-task", taskName)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}

func TestReadCurrentNoLock(t *testing.T) {
	dir := t.TempDir()

	_, _, err := ReadCurrent(dir)
	if err == nil {
		t.Fatal("expected error when no lock exists")
	}
}

func TestReadCurrentStaleLock(t *testing.T) {
	dir := t.TempDir()

	stalePID := 4194304
	data, err := json.Marshal(&lockData{PID: stalePID, TaskName: "stale"})
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(dir, LockFile), data, 0o600))

	_, _, err = ReadCurrent(dir)
	if err == nil {
		t.Fatal("expected error for stale lock")
	}
}
