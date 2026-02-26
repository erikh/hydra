package design

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupDesignDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	must(t, os.WriteFile(filepath.Join(dir, "rules.md"), []byte("Use Go idioms."), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "lint.md"), []byte("Run gofmt."), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "functional.md"), []byte("All tests pass."), 0o600))

	must(t, os.MkdirAll(filepath.Join(dir, "tasks"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "add-auth.md"), []byte("Add authentication."), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "fix-bug.md"), []byte("Fix the login bug."), 0o600))

	must(t, os.MkdirAll(filepath.Join(dir, "tasks", "backend"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "backend", "add-api.md"), []byte("Add REST API."), 0o600))

	must(t, os.MkdirAll(filepath.Join(dir, "state", "review"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "state", "review", "old-task.md"), []byte("Done."), 0o600))

	must(t, os.MkdirAll(filepath.Join(dir, "state", "completed"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "state", "completed", "shipped.md"), []byte("Shipped."), 0o600))

	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestScaffoldCreatesStructure(t *testing.T) {
	dir := t.TempDir()

	if err := Scaffold(dir); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	// Verify all directories exist.
	for _, d := range []string{
		"tasks",
		"other",
		filepath.Join("state", "review"),
		filepath.Join("state", "merge"),
		filepath.Join("state", "completed"),
		filepath.Join("state", "abandoned"),
		filepath.Join("milestone", "history"),
	} {
		info, err := os.Stat(filepath.Join(dir, d))
		if err != nil {
			t.Errorf("directory %s not created: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", d)
		}
	}

	// Verify all files exist.
	for _, f := range []string{
		"rules.md",
		"lint.md",
		"functional.md",
		"hydra.yml",
		filepath.Join("state", "record.json"),
	} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("file %s not created: %v", f, err)
		}
	}

	// Verify record.json contains empty array.
	data, err := os.ReadFile(filepath.Join(dir, "state", "record.json")) //nolint:gosec // test
	if err != nil {
		t.Fatalf("reading record.json: %v", err)
	}
	if string(data) != "[]\n" {
		t.Errorf("record.json = %q, want %q", string(data), "[]\n")
	}
}

func TestScaffoldSkipsExisting(t *testing.T) {
	dir := t.TempDir()

	// Create rules.md with content before scaffolding.
	must(t, os.WriteFile(filepath.Join(dir, "rules.md"), []byte("My custom rules."), 0o600))

	if err := Scaffold(dir); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	// Verify rules.md content is preserved.
	data, err := os.ReadFile(filepath.Join(dir, "rules.md")) //nolint:gosec // test
	if err != nil {
		t.Fatalf("reading rules.md: %v", err)
	}
	if string(data) != "My custom rules." {
		t.Errorf("rules.md = %q, want %q", string(data), "My custom rules.")
	}

	// Verify other scaffold files were NOT created (since we skipped).
	if _, err := os.Stat(filepath.Join(dir, "hydra.yml")); !os.IsNotExist(err) {
		t.Error("hydra.yml should not exist when scaffolding is skipped")
	}
}

func TestNewDir(t *testing.T) {
	dir := setupDesignDir(t)

	dd, err := NewDir(dir)
	if err != nil {
		t.Fatalf("NewDir: %v", err)
	}

	if dd.Path == "" {
		t.Fatal("Path is empty")
	}
}

func TestNewDirNotExist(t *testing.T) {
	_, err := NewDir("/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for non-existent dir")
	}
}

func TestNewDirIsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	must(t, os.WriteFile(f, []byte("hi"), 0o600))

	_, err := NewDir(f)
	if err == nil {
		t.Fatal("expected error for file instead of dir")
	}
}

func TestRules(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	rules, err := dd.Rules()
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	if rules != "Use Go idioms." {
		t.Errorf("Rules = %q", rules)
	}
}

func TestLint(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	lint, err := dd.Lint()
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if lint != "Run gofmt." {
		t.Errorf("Lint = %q", lint)
	}
}

func TestFunctional(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	fn, err := dd.Functional()
	if err != nil {
		t.Fatalf("Functional: %v", err)
	}
	if fn != "All tests pass." {
		t.Errorf("Functional = %q", fn)
	}
}

func TestMissingOptionalFiles(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "tasks"), 0o750))

	dd, _ := NewDir(dir)

	rules, err := dd.Rules()
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	if rules != "" {
		t.Errorf("expected empty rules, got %q", rules)
	}

	lint, err := dd.Lint()
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if lint != "" {
		t.Errorf("expected empty lint, got %q", lint)
	}

	fn, err := dd.Functional()
	if err != nil {
		t.Fatalf("Functional: %v", err)
	}
	if fn != "" {
		t.Errorf("expected empty functional, got %q", fn)
	}
}

func TestAssembleDocumentFull(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	doc, err := dd.AssembleDocument("Build the widget.", "")
	if err != nil {
		t.Fatalf("AssembleDocument: %v", err)
	}

	if !strings.Contains(doc, "# Rules") {
		t.Error("missing Rules section")
	}
	if !strings.Contains(doc, "Use Go idioms.") {
		t.Error("missing rules content")
	}
	if !strings.Contains(doc, "# Lint Rules") {
		t.Error("missing Lint Rules section")
	}
	if !strings.Contains(doc, "Run gofmt.") {
		t.Error("missing lint content")
	}
	if !strings.Contains(doc, "# Task") {
		t.Error("missing Task section")
	}
	if !strings.Contains(doc, "Build the widget.") {
		t.Error("missing task content")
	}
	if !strings.Contains(doc, "# Functional Tests") {
		t.Error("missing Functional Tests section")
	}
	if !strings.Contains(doc, "All tests pass.") {
		t.Error("missing functional content")
	}

	// Verify ordering: Rules before Lint before Task before Functional.
	rulesIdx := strings.Index(doc, "# Rules")
	lintIdx := strings.Index(doc, "# Lint Rules")
	taskIdx := strings.Index(doc, "# Task")
	funcIdx := strings.Index(doc, "# Functional Tests")

	if rulesIdx >= lintIdx || lintIdx >= taskIdx || taskIdx >= funcIdx {
		t.Error("sections are not in the correct order")
	}
}

func TestAssembleDocumentMinimal(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "tasks"), 0o750))

	dd, _ := NewDir(dir)

	doc, err := dd.AssembleDocument("Do something.", "")
	if err != nil {
		t.Fatalf("AssembleDocument: %v", err)
	}

	if strings.Contains(doc, "# Rules") {
		t.Error("should not include empty Rules section")
	}
	if strings.Contains(doc, "# Lint Rules") {
		t.Error("should not include empty Lint section")
	}
	if !strings.Contains(doc, "# Task") {
		t.Error("missing Task section")
	}
	if strings.Contains(doc, "# Functional Tests") {
		t.Error("should not include empty Functional section")
	}
}

func TestPendingTasks(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	tasks, err := dd.PendingTasks()
	if err != nil {
		t.Fatalf("PendingTasks: %v", err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 pending tasks, got %d", len(tasks))
	}

	names := map[string]bool{}
	for _, task := range tasks {
		key := task.Name
		if task.Group != "" {
			key = task.Group + "/" + task.Name
		}
		names[key] = true
	}

	for _, want := range []string{"add-auth", "fix-bug", "backend/add-api"} {
		if !names[want] {
			t.Errorf("missing task %q", want)
		}
	}
}

func TestPendingTasksEmptyDir(t *testing.T) {
	dir := t.TempDir()
	dd, _ := NewDir(dir)

	tasks, err := dd.PendingTasks()
	if err != nil {
		t.Fatalf("PendingTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestTasksByState(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	review, err := dd.TasksByState(StateReview)
	if err != nil {
		t.Fatalf("TasksByState review: %v", err)
	}
	if len(review) != 1 || review[0].Name != "old-task" {
		t.Errorf("review tasks = %v", review)
	}

	completed, err := dd.TasksByState(StateCompleted)
	if err != nil {
		t.Fatalf("TasksByState completed: %v", err)
	}
	if len(completed) != 1 || completed[0].Name != "shipped" {
		t.Errorf("completed tasks = %v", completed)
	}

	// Empty states return empty slice.
	merge, err := dd.TasksByState(StateMerge)
	if err != nil {
		t.Fatalf("TasksByState merge: %v", err)
	}
	if len(merge) != 0 {
		t.Errorf("expected 0 merge tasks, got %d", len(merge))
	}
}

func TestTasksByStateUnknown(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	_, err := dd.TasksByState("bogus")
	if err == nil {
		t.Fatal("expected error for unknown state")
	}
}

func TestAllTasks(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	all, err := dd.AllTasks()
	if err != nil {
		t.Fatalf("AllTasks: %v", err)
	}

	// 3 pending + 1 review + 1 completed = 5.
	if len(all) != 5 {
		t.Errorf("expected 5 total tasks, got %d", len(all))
	}
}

func TestFindTask(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	// Find by plain name.
	task, err := dd.FindTask("add-auth")
	if err != nil {
		t.Fatalf("FindTask add-auth: %v", err)
	}
	if task.Name != "add-auth" {
		t.Errorf("Name = %q", task.Name)
	}
	if task.Group != "" {
		t.Errorf("Group = %q, want empty", task.Group)
	}

	// Find by group/name.
	task, err = dd.FindTask("backend/add-api")
	if err != nil {
		t.Fatalf("FindTask backend/add-api: %v", err)
	}
	if task.Name != "add-api" {
		t.Errorf("Name = %q", task.Name)
	}
	if task.Group != "backend" {
		t.Errorf("Group = %q", task.Group)
	}
}

func TestFindTaskNotFound(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	_, err := dd.FindTask("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestTaskContent(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	task, _ := dd.FindTask("add-auth")
	content, err := task.Content()
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if content != "Add authentication." {
		t.Errorf("Content = %q", content)
	}
}

func TestBranchName(t *testing.T) {
	tests := []struct {
		name  string
		group string
		want  string
	}{
		{"add-auth", "", "hydra/add-auth"},
		{"Add Auth", "", "hydra/add-auth"},
		{"add-api", "backend", "hydra/backend/add-api"},
		{"My Task", "Frontend", "hydra/frontend/my-task"},
	}

	for _, tt := range tests {
		task := &Task{Name: tt.name, Group: tt.group}
		got := task.BranchName()
		if got != tt.want {
			t.Errorf("BranchName(%q, group=%q) = %q, want %q", tt.name, tt.group, got, tt.want)
		}
	}
}

func TestMoveTask(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	task, _ := dd.FindTask("add-auth")
	originalPath := task.FilePath

	if err := dd.MoveTask(task, StateReview); err != nil {
		t.Fatalf("MoveTask: %v", err)
	}

	// Original file should be gone.
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Error("original file still exists")
	}

	// Task should be at new location.
	if _, err := os.Stat(task.FilePath); err != nil {
		t.Errorf("new file doesn't exist: %v", err)
	}

	if task.State != StateReview {
		t.Errorf("State = %q, want review", task.State)
	}

	expectedDir := filepath.Join(dir, "state", "review")
	if filepath.Dir(task.FilePath) != expectedDir {
		t.Errorf("FilePath dir = %q, want %q", filepath.Dir(task.FilePath), expectedDir)
	}
}

func TestMoveTaskAllStates(t *testing.T) {
	for _, state := range []TaskState{StateReview, StateMerge, StateCompleted, StateAbandoned} {
		t.Run(string(state), func(t *testing.T) {
			dir := setupDesignDir(t)
			dd, _ := NewDir(dir)

			task, _ := dd.FindTask("fix-bug")
			if err := dd.MoveTask(task, state); err != nil {
				t.Fatalf("MoveTask to %s: %v", state, err)
			}
			if task.State != state {
				t.Errorf("State = %q, want %q", task.State, state)
			}
		})
	}
}

func TestMoveTaskInvalidState(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	task, _ := dd.FindTask("fix-bug")
	err := dd.MoveTask(task, "bogus")
	if err == nil {
		t.Fatal("expected error for invalid state")
	}
}

func TestNonMdFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "tasks"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "real-task.md"), []byte("task"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "notes.txt"), []byte("not a task"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", ".hidden"), []byte("hidden"), 0o600))

	dd, _ := NewDir(dir)
	tasks, err := dd.PendingTasks()
	if err != nil {
		t.Fatalf("PendingTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "real-task" {
		t.Errorf("Name = %q, want real-task", tasks[0].Name)
	}
}

func TestRecordAddAndEntries(t *testing.T) {
	dir := t.TempDir()

	rec := NewRecord(dir)

	// Initially empty.
	entries, err := rec.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}

	// Add entries.
	if err := rec.Add("abc123", "add-feature"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := rec.Add("def456", "fix-bug"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err = rec.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].SHA != "abc123" || entries[0].TaskName != "add-feature" {
		t.Errorf("entry[0] = %+v", entries[0])
	}
	if entries[1].SHA != "def456" || entries[1].TaskName != "fix-bug" {
		t.Errorf("entry[1] = %+v", entries[1])
	}
}

func TestMilestones(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-01-15.md"), []byte("Q1 milestone"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"), []byte("Q2 milestone"), 0o600))

	dd, _ := NewDir(dir)
	milestones, err := dd.Milestones()
	if err != nil {
		t.Fatalf("Milestones: %v", err)
	}
	if len(milestones) != 2 {
		t.Fatalf("expected 2 milestones, got %d", len(milestones))
	}

	dates := map[string]bool{}
	for _, m := range milestones {
		dates[m.Date] = true
	}
	if !dates["2025-01-15"] || !dates["2025-06-01"] {
		t.Errorf("milestones = %v", milestones)
	}
}

func TestMilestonesEmpty(t *testing.T) {
	dir := t.TempDir()
	dd, _ := NewDir(dir)

	milestones, err := dd.Milestones()
	if err != nil {
		t.Fatalf("Milestones: %v", err)
	}
	if len(milestones) != 0 {
		t.Errorf("expected 0 milestones, got %d", len(milestones))
	}
}

func TestMilestoneHistory(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone", "history"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "history", "2025-01-15-A.md"), []byte("Great"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "history", "2025-06-01-C.md"), []byte("OK"), 0o600))

	dd, _ := NewDir(dir)
	history, err := dd.MilestoneHistory()
	if err != nil {
		t.Fatalf("MilestoneHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}

	found := map[string]string{}
	for _, h := range history {
		found[h.Date] = h.Score
	}
	if found["2025-01-15"] != "A" {
		t.Errorf("2025-01-15 score = %q, want A", found["2025-01-15"])
	}
	if found["2025-06-01"] != "C" {
		t.Errorf("2025-06-01 score = %q, want C", found["2025-06-01"])
	}
}

func TestMilestoneHistoryEmpty(t *testing.T) {
	dir := t.TempDir()
	dd, _ := NewDir(dir)

	history, err := dd.MilestoneHistory()
	if err != nil {
		t.Fatalf("MilestoneHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected 0 history entries, got %d", len(history))
	}
}

func writeMockEditor(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "mock-editor-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' '%s' > \"$1\"\n", content)
	if _, err := f.WriteString(script); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil { //nolint:gosec // must be executable
		t.Fatal(err)
	}
	return f.Name()
}

func writeMockEditorFailing(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "mock-editor-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("#!/bin/sh\nexit 1\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil { //nolint:gosec // must be executable
		t.Fatal(err)
	}
	return f.Name()
}

func writeMockEditorNoop(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "mock-editor-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Does nothing — leaves the file empty.
	if _, err := f.WriteString("#!/bin/sh\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil { //nolint:gosec // must be executable
		t.Fatal(err)
	}
	return f.Name()
}

func TestEditNewTask(t *testing.T) {
	dir := t.TempDir()
	editor := writeMockEditor(t, "task content")

	err := EditNewTask(dir, "my-task", editor, nil, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("EditNewTask: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "tasks", "my-task.md")) //nolint:gosec // test
	if err != nil {
		t.Fatalf("reading task file: %v", err)
	}
	if string(data) != "task content" {
		t.Errorf("task content = %q, want %q", string(data), "task content")
	}
}

func TestEditNewTaskEmptyFile(t *testing.T) {
	dir := t.TempDir()
	editor := writeMockEditorNoop(t)

	err := EditNewTask(dir, "my-task", editor, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q, want message about empty", err)
	}

	// File should not exist.
	if _, err := os.Stat(filepath.Join(dir, "tasks", "my-task.md")); !os.IsNotExist(err) {
		t.Error("task file should not exist")
	}
}

func TestEditNewTaskEditorFails(t *testing.T) {
	dir := t.TempDir()
	editor := writeMockEditorFailing(t)

	err := EditNewTask(dir, "my-task", editor, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error when editor fails")
	}

	// File should not exist.
	if _, err := os.Stat(filepath.Join(dir, "tasks", "my-task.md")); !os.IsNotExist(err) {
		t.Error("task file should not exist")
	}
}

func TestEditNewTaskAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "tasks"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "my-task.md"), []byte("existing"), 0o600))

	editor := writeMockEditor(t, "new content")

	err := EditNewTask(dir, "my-task", editor, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error when task already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want already exists", err)
	}
}

func TestEditNewTaskSlashRejected(t *testing.T) {
	dir := t.TempDir()
	editor := writeMockEditor(t, "content")

	err := EditNewTask(dir, "group/my-task", editor, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for task name with slash")
	}
	if !strings.Contains(err.Error(), "/") {
		t.Errorf("error = %q, want message about slash", err)
	}
}

func TestGroupContent(t *testing.T) {
	dir := setupDesignDir(t)
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "backend", "group.md"), []byte("Backend group context."), 0o600))

	dd, _ := NewDir(dir)
	content, err := dd.GroupContent("backend")
	if err != nil {
		t.Fatalf("GroupContent: %v", err)
	}
	if content != "Backend group context." {
		t.Errorf("GroupContent = %q, want %q", content, "Backend group context.")
	}
}

func TestGroupContentMissing(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	content, err := dd.GroupContent("backend")
	if err != nil {
		t.Fatalf("GroupContent: %v", err)
	}
	if content != "" {
		t.Errorf("GroupContent = %q, want empty", content)
	}
}

func TestGroupContentEmptyGroup(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	content, err := dd.GroupContent("")
	if err != nil {
		t.Fatalf("GroupContent: %v", err)
	}
	if content != "" {
		t.Errorf("GroupContent = %q, want empty", content)
	}
}

func TestAssembleDocumentWithGroup(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	doc, err := dd.AssembleDocument("Build the widget.", "Backend group context.")
	if err != nil {
		t.Fatalf("AssembleDocument: %v", err)
	}

	if !strings.Contains(doc, "# Group") {
		t.Error("missing Group section")
	}
	if !strings.Contains(doc, "Backend group context.") {
		t.Error("missing group content")
	}

	// Verify ordering: Lint before Group before Task.
	lintIdx := strings.Index(doc, "# Lint Rules")
	groupIdx := strings.Index(doc, "# Group")
	taskIdx := strings.Index(doc, "# Task")

	if lintIdx >= groupIdx || groupIdx >= taskIdx {
		t.Error("Group section is not between Lint and Task")
	}
}

func TestAssembleDocumentWithoutGroup(t *testing.T) {
	dir := setupDesignDir(t)
	dd, _ := NewDir(dir)

	doc, err := dd.AssembleDocument("Build the widget.", "")
	if err != nil {
		t.Fatalf("AssembleDocument: %v", err)
	}

	if strings.Contains(doc, "# Group") {
		t.Error("should not include Group section when group content is empty")
	}
}

func TestPendingTasksSkipsGroupMd(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "tasks", "mygroup"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "mygroup", "group.md"), []byte("heading"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "tasks", "mygroup", "real-task.md"), []byte("task"), 0o600))

	dd, _ := NewDir(dir)
	tasks, err := dd.PendingTasks()
	if err != nil {
		t.Fatalf("PendingTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "real-task" {
		t.Errorf("Name = %q, want real-task", tasks[0].Name)
	}
}

func TestRecordPersistence(t *testing.T) {
	dir := t.TempDir()

	rec := NewRecord(dir)
	if err := rec.Add("sha1", "task1"); err != nil {
		t.Fatal(err)
	}

	// Create a new record instance pointing to the same file.
	rec2 := NewRecord(dir)
	entries, err := rec2.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].SHA != "sha1" {
		t.Errorf("SHA = %q, want sha1", entries[0].SHA)
	}
}
