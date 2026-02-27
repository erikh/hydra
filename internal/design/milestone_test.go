package design

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Ship user authentication", "ship-user-authentication"},
		{"Reach 80% code coverage", "reach-80-code-coverage"},
		{"  Leading/trailing  ", "leading-trailing"},
		{"UPPER CASE", "upper-case"},
		{"simple", "simple"},
		{"a--b--c", "a-b-c"},
		{"", ""},
	}

	for _, tt := range tests {
		got := Slugify(tt.input)
		if got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSlugifyLongInput(t *testing.T) {
	long := "this is a very long heading that should be truncated at sixty characters to avoid filename issues"
	got := Slugify(long)
	if len(got) > 60 {
		t.Errorf("Slugify produced slug of length %d, want <= 60", len(got))
	}
	if got[len(got)-1] == '-' {
		t.Errorf("Slugify result ends with hyphen: %q", got)
	}
}

func TestNormalizeDateValid(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2025-06-15", "2025-06-15"},
		{"2025/06/15", "2025-06-15"},
		{"06-15-2025", "2025-06-15"},
		{"06/15/2025", "2025-06-15"},
	}

	for _, tt := range tests {
		got, err := NormalizeDate(tt.input)
		if err != nil {
			t.Errorf("NormalizeDate(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeDate(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeDateInvalid(t *testing.T) {
	invalid := []string{
		"not-a-date",
		"2025",
		"15-06-2025",
		"2025.06.15",
		"",
	}

	for _, input := range invalid {
		_, err := NormalizeDate(input)
		if err == nil {
			t.Errorf("NormalizeDate(%q) expected error, got nil", input)
		}
	}
}

func TestParsePromisesMultiHeading(t *testing.T) {
	content := `## Ship authentication
Implement login and registration.

## Add tests
Cover all endpoints.
`
	promises := ParsePromises(content)
	if len(promises) != 2 {
		t.Fatalf("expected 2 promises, got %d", len(promises))
	}

	if promises[0].Heading != "Ship authentication" {
		t.Errorf("promises[0].Heading = %q", promises[0].Heading)
	}
	if promises[0].Slug != "ship-authentication" {
		t.Errorf("promises[0].Slug = %q", promises[0].Slug)
	}
	if promises[0].Body != "Implement login and registration." {
		t.Errorf("promises[0].Body = %q", promises[0].Body)
	}

	if promises[1].Heading != "Add tests" {
		t.Errorf("promises[1].Heading = %q", promises[1].Heading)
	}
	if promises[1].Body != "Cover all endpoints." {
		t.Errorf("promises[1].Body = %q", promises[1].Body)
	}
}

func TestParsePromisesSingle(t *testing.T) {
	content := "## Only one\nSome details.\n"
	promises := ParsePromises(content)
	if len(promises) != 1 {
		t.Fatalf("expected 1 promise, got %d", len(promises))
	}
	if promises[0].Heading != "Only one" {
		t.Errorf("Heading = %q", promises[0].Heading)
	}
}

func TestParsePromisesEmpty(t *testing.T) {
	promises := ParsePromises("")
	if len(promises) != 0 {
		t.Errorf("expected 0 promises, got %d", len(promises))
	}
}

func TestParsePromisesWithHTMLComments(t *testing.T) {
	content := `<!-- This is a comment with ## fake heading -->

## Real heading
Real content.

<!-- Another comment -->

## Second heading
More content.
`
	promises := ParsePromises(content)
	if len(promises) != 2 {
		t.Fatalf("expected 2 promises, got %d", len(promises))
	}
	if promises[0].Heading != "Real heading" {
		t.Errorf("promises[0].Heading = %q", promises[0].Heading)
	}
	if promises[1].Heading != "Second heading" {
		t.Errorf("promises[1].Heading = %q", promises[1].Heading)
	}
}

func TestParsePromisesEmptyHeading(t *testing.T) {
	content := "## \nSome body.\n## Real heading\nContent.\n"
	promises := ParsePromises(content)
	if len(promises) != 1 {
		t.Fatalf("expected 1 promise (empty heading skipped), got %d", len(promises))
	}
	if promises[0].Heading != "Real heading" {
		t.Errorf("Heading = %q", promises[0].Heading)
	}
}

func TestMilestoneContent(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"), []byte("## Ship it\nDetails."), 0o600))

	m := &Milestone{
		Date:     "2025-06-01",
		FilePath: filepath.Join(dir, "milestone", "2025-06-01.md"),
	}

	content, err := m.Content()
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if content != "## Ship it\nDetails." {
		t.Errorf("Content = %q", content)
	}
}

func TestFindMilestone(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"), []byte("content"), 0o600))

	dd, _ := NewDir(dir)

	m, err := dd.FindMilestone("2025-06-01")
	if err != nil {
		t.Fatalf("FindMilestone: %v", err)
	}
	if m.Date != "2025-06-01" {
		t.Errorf("Date = %q", m.Date)
	}

	_, err = dd.FindMilestone("2025-12-31")
	if err == nil {
		t.Fatal("expected error for missing milestone")
	}
}

func TestDeliveredMilestonesPopulated(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone", "delivered"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "delivered", "2025-01-01.md"), []byte("done"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "delivered", "2025-03-01.md"), []byte("done"), 0o600))

	dd, _ := NewDir(dir)
	delivered, err := dd.DeliveredMilestones()
	if err != nil {
		t.Fatalf("DeliveredMilestones: %v", err)
	}
	if len(delivered) != 2 {
		t.Fatalf("expected 2, got %d", len(delivered))
	}
}

func TestDeliveredMilestonesEmpty(t *testing.T) {
	dir := t.TempDir()
	dd, _ := NewDir(dir)

	delivered, err := dd.DeliveredMilestones()
	if err != nil {
		t.Fatalf("DeliveredMilestones: %v", err)
	}
	if len(delivered) != 0 {
		t.Errorf("expected 0, got %d", len(delivered))
	}
}

func TestDeliverMilestone(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.MkdirAll(filepath.Join(dir, "milestone", "delivered"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"), []byte("content"), 0o600))

	dd, _ := NewDir(dir)
	m, _ := dd.FindMilestone("2025-06-01")

	if err := dd.DeliverMilestone(m); err != nil {
		t.Fatalf("DeliverMilestone: %v", err)
	}

	// Original should be gone.
	if _, err := os.Stat(filepath.Join(dir, "milestone", "2025-06-01.md")); !os.IsNotExist(err) {
		t.Error("original file still exists")
	}

	// Should be in delivered/.
	if _, err := os.Stat(filepath.Join(dir, "milestone", "delivered", "2025-06-01.md")); err != nil {
		t.Errorf("delivered file missing: %v", err)
	}
}

func TestCreateMilestoneSuccess(t *testing.T) {
	dir := t.TempDir()
	dd, _ := NewDir(dir)

	m, err := dd.CreateMilestone("2025-06-01", "## Promise\nDetails.\n")
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}

	if m.Date != "2025-06-01" {
		t.Errorf("Date = %q", m.Date)
	}

	content, err := m.Content()
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if content != "## Promise\nDetails.\n" {
		t.Errorf("Content = %q", content)
	}
}

func TestCreateMilestoneDuplicate(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"), []byte("existing"), 0o600))

	dd, _ := NewDir(dir)
	_, err := dd.CreateMilestone("2025-06-01", "new content")
	if err == nil {
		t.Fatal("expected error for duplicate milestone")
	}
}

func TestVerifyMilestoneAllKept(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"),
		[]byte("## Ship auth\nLogin flow.\n\n## Add tests\nCoverage.\n"), 0o600))

	// Create tasks in the milestone group, then move to completed.
	groupDir := filepath.Join(dir, "tasks", "milestone-2025-06-01")
	must(t, os.MkdirAll(groupDir, 0o750))
	must(t, os.WriteFile(filepath.Join(groupDir, "group.md"), []byte("Milestone tasks."), 0o600))
	must(t, os.WriteFile(filepath.Join(groupDir, "ship-auth.md"), []byte("done"), 0o600))
	must(t, os.WriteFile(filepath.Join(groupDir, "add-tests.md"), []byte("done"), 0o600))

	dd, _ := NewDir(dir)
	task1, _ := dd.FindTask("milestone-2025-06-01/ship-auth")
	must(t, dd.MoveTask(task1, StateCompleted))
	task2, _ := dd.FindTask("milestone-2025-06-01/add-tests")
	must(t, dd.MoveTask(task2, StateCompleted))

	m, _ := dd.FindMilestone("2025-06-01")
	result, err := dd.VerifyMilestone(m)
	if err != nil {
		t.Fatalf("VerifyMilestone: %v", err)
	}

	if !result.AllKept {
		t.Errorf("AllKept=%v, Missing=%v, Incomplete=%v", result.AllKept, result.Missing, result.Incomplete)
	}
}

func TestVerifyMilestoneIncomplete(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"),
		[]byte("## Ship auth\nLogin.\n\n## Add tests\nCoverage.\n"), 0o600))

	// Create tasks as pending in the group (incomplete).
	groupDir := filepath.Join(dir, "tasks", "milestone-2025-06-01")
	must(t, os.MkdirAll(groupDir, 0o750))
	must(t, os.WriteFile(filepath.Join(groupDir, "group.md"), []byte("Milestone."), 0o600))
	must(t, os.WriteFile(filepath.Join(groupDir, "ship-auth.md"), []byte("task"), 0o600))
	must(t, os.WriteFile(filepath.Join(groupDir, "add-tests.md"), []byte("task"), 0o600))

	dd, _ := NewDir(dir)
	m, _ := dd.FindMilestone("2025-06-01")
	result, err := dd.VerifyMilestone(m)
	if err != nil {
		t.Fatalf("VerifyMilestone: %v", err)
	}

	if result.AllKept {
		t.Error("expected AllKept=false for pending tasks")
	}
	if len(result.Incomplete) != 2 {
		t.Errorf("expected 2 incomplete, got %d: %v", len(result.Incomplete), result.Incomplete)
	}
}

func TestVerifyMilestoneMissing(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"),
		[]byte("## Ship auth\nLogin.\n\n## Add tests\nCoverage.\n"), 0o600))

	// No tasks at all.
	must(t, os.MkdirAll(filepath.Join(dir, "tasks"), 0o750))

	dd, _ := NewDir(dir)
	m, _ := dd.FindMilestone("2025-06-01")
	result, err := dd.VerifyMilestone(m)
	if err != nil {
		t.Fatalf("VerifyMilestone: %v", err)
	}

	if result.AllKept {
		t.Error("expected AllKept=false")
	}
	if len(result.Missing) != 2 {
		t.Errorf("expected 2 missing, got %d: %v", len(result.Missing), result.Missing)
	}
}

func TestRepairMilestoneCreatesMissing(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"),
		[]byte("## Ship auth\nLogin flow.\n\n## Add tests\nCoverage.\n"), 0o600))
	must(t, os.MkdirAll(filepath.Join(dir, "tasks"), 0o750))

	dd, _ := NewDir(dir)
	m, _ := dd.FindMilestone("2025-06-01")

	result, err := dd.RepairMilestone(m)
	if err != nil {
		t.Fatalf("RepairMilestone: %v", err)
	}

	if len(result.Created) != 2 {
		t.Fatalf("expected 2 created, got %d", len(result.Created))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}

	// Verify task files exist.
	groupDir := filepath.Join(dir, "tasks", "milestone-2025-06-01")
	for _, slug := range []string{"ship-auth", "add-tests"} {
		if _, err := os.Stat(filepath.Join(groupDir, slug+".md")); err != nil {
			t.Errorf("task file %s not created: %v", slug, err)
		}
	}

	// Verify group.md exists.
	if _, err := os.Stat(filepath.Join(groupDir, "group.md")); err != nil {
		t.Error("group.md not created")
	}
}

func TestRepairMilestoneSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "milestone"), 0o750))
	must(t, os.WriteFile(filepath.Join(dir, "milestone", "2025-06-01.md"),
		[]byte("## Ship auth\nLogin.\n\n## Add tests\nCoverage.\n"), 0o600))

	// Pre-create one task.
	groupDir := filepath.Join(dir, "tasks", "milestone-2025-06-01")
	must(t, os.MkdirAll(groupDir, 0o750))
	must(t, os.WriteFile(filepath.Join(groupDir, "group.md"), []byte("Milestone."), 0o600))
	must(t, os.WriteFile(filepath.Join(groupDir, "ship-auth.md"), []byte("existing"), 0o600))

	dd, _ := NewDir(dir)
	m, _ := dd.FindMilestone("2025-06-01")

	result, err := dd.RepairMilestone(m)
	if err != nil {
		t.Fatalf("RepairMilestone: %v", err)
	}

	if len(result.Created) != 1 {
		t.Errorf("expected 1 created, got %d: %v", len(result.Created), result.Created)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}

	// Verify existing file was NOT overwritten.
	data, err := os.ReadFile(filepath.Join(groupDir, "ship-auth.md")) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing" {
		t.Errorf("existing task was overwritten: %q", string(data))
	}
}

func TestMilestoneTaskGroup(t *testing.T) {
	got := MilestoneTaskGroup("2025-06-01")
	if got != "milestone-2025-06-01" {
		t.Errorf("MilestoneTaskGroup = %q", got)
	}
}
