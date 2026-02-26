package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()

	lk := New(dir, "test-task")
	if err := lk.Acquire(); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Lock file should exist
	lockPath := filepath.Join(dir, LockFile)
	data, err := os.ReadFile(lockPath)
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

	if err := lk.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file still exists after Release")
	}
}

func TestAcquireBlockedByLiveLock(t *testing.T) {
	dir := t.TempDir()

	// First lock (our own PID, so it's alive)
	lk1 := New(dir, "task-1")
	if err := lk1.Acquire(); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	// Second lock should fail
	lk2 := New(dir, "task-2")
	err := lk2.Acquire()
	if err == nil {
		t.Fatal("expected error when lock is held by live process")
	}

	lk1.Release()
}

func TestAcquireStaleLock(t *testing.T) {
	dir := t.TempDir()

	// Write a lock with a PID that definitely doesn't exist
	// PID 2^22 is unlikely to be running and is valid on Linux
	stalePID := 4194304
	data, _ := json.Marshal(&lockData{PID: stalePID, TaskName: "stale-task"})
	os.WriteFile(filepath.Join(dir, LockFile), data, 0o644)

	lk := New(dir, "new-task")
	if err := lk.Acquire(); err != nil {
		t.Fatalf("Acquire with stale lock should succeed: %v", err)
	}

	// Verify we now hold the lock
	readData, _ := os.ReadFile(filepath.Join(dir, LockFile))
	var ld lockData
	json.Unmarshal(readData, &ld)

	if ld.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", ld.PID, os.Getpid())
	}
	if ld.TaskName != "new-task" {
		t.Errorf("TaskName = %q, want new-task", ld.TaskName)
	}

	lk.Release()
}

func TestReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()

	lk := New(dir, "test-task")
	// Release without Acquire should not error
	if err := lk.Release(); err != nil {
		t.Fatalf("Release without Acquire: %v", err)
	}

	// Double release
	lk.Acquire()
	lk.Release()
	if err := lk.Release(); err != nil {
		t.Fatalf("double Release: %v", err)
	}
}

func TestReadCurrent(t *testing.T) {
	dir := t.TempDir()

	lk := New(dir, "running-task")
	lk.Acquire()
	defer lk.Release()

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
	data, _ := json.Marshal(&lockData{PID: stalePID, TaskName: "stale"})
	os.WriteFile(filepath.Join(dir, LockFile), data, 0o644)

	_, _, err := ReadCurrent(dir)
	if err == nil {
		t.Fatal("expected error for stale lock")
	}
}
