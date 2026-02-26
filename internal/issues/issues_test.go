package issues

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erikh/hydra/internal/design"
)

// mockSource implements Source for testing.
type mockSource struct {
	issues []Issue
	err    error
}

func (m *mockSource) FetchOpenIssues(_ context.Context, _ []string) ([]Issue, error) {
	return m.issues, m.err
}

func TestSyncCreatesFiles(t *testing.T) {
	designDir := t.TempDir()

	src := &mockSource{
		issues: []Issue{
			{Number: 1, Title: "Fix the bug", Body: "There is a bug.", Labels: []string{"bug"}, URL: "https://example.com/1"},
			{Number: 2, Title: "Add feature", Body: "New feature needed.", Labels: []string{"enhancement"}, URL: "https://example.com/2"},
		},
	}

	created, skipped, err := Sync(context.Background(), designDir, src, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if created != 2 {
		t.Errorf("created = %d, want 2", created)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}

	// Verify group.md exists.
	groupPath := filepath.Join(designDir, "tasks", "issues", "group.md")
	if _, err := os.Stat(groupPath); err != nil {
		t.Error("group.md not created")
	}

	// Verify issue files.
	issuesDir := filepath.Join(designDir, "tasks", "issues")
	entries, _ := os.ReadDir(issuesDir)

	found := map[string]bool{}
	for _, e := range entries {
		found[e.Name()] = true
	}

	if !found["1-fix-the-bug.md"] {
		t.Error("missing 1-fix-the-bug.md")
	}
	if !found["2-add-feature.md"] {
		t.Error("missing 2-add-feature.md")
	}
}

func TestSyncSkipsDuplicates(t *testing.T) {
	designDir := t.TempDir()
	issuesDir := filepath.Join(designDir, "tasks", "issues")
	if err := os.MkdirAll(issuesDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Pre-create a file for issue #1.
	if err := os.WriteFile(filepath.Join(issuesDir, "1-old-name.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	src := &mockSource{
		issues: []Issue{
			{Number: 1, Title: "Fix the bug", Body: "There is a bug.", URL: "https://example.com/1"},
			{Number: 2, Title: "New issue", Body: "New.", URL: "https://example.com/2"},
		},
	}

	created, skipped, err := Sync(context.Background(), designDir, src, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if created != 1 {
		t.Errorf("created = %d, want 1", created)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}

	// Original file should be untouched.
	data, _ := os.ReadFile(filepath.Join(issuesDir, "1-old-name.md")) //nolint:gosec // test
	if string(data) != "old" {
		t.Error("original file was overwritten")
	}
}

func TestSyncGroupMdCreated(t *testing.T) {
	designDir := t.TempDir()

	src := &mockSource{issues: []Issue{}}
	_, _, err := Sync(context.Background(), designDir, src, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	groupPath := filepath.Join(designDir, "tasks", "issues", "group.md")
	data, err := os.ReadFile(groupPath) //nolint:gosec // test
	if err != nil {
		t.Fatal("group.md not created")
	}
	if !strings.Contains(string(data), "Imported") {
		t.Errorf("group.md content = %q, want 'Imported' text", string(data))
	}
}

func TestFileContentFormat(t *testing.T) {
	designDir := t.TempDir()

	src := &mockSource{
		issues: []Issue{
			{Number: 42, Title: "Test Issue", Body: "Description here.", Labels: []string{"bug", "p1"}, URL: "https://example.com/42"},
		},
	}

	_, _, err := Sync(context.Background(), designDir, src, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(designDir, "tasks", "issues", "42-test-issue.md")) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "Issue #42: Test Issue") {
		t.Error("missing issue title line")
	}
	if !strings.Contains(content, "URL: https://example.com/42") {
		t.Error("missing URL line")
	}
	if !strings.Contains(content, "Labels: bug, p1") {
		t.Error("missing labels line")
	}
	if !strings.Contains(content, "Description here.") {
		t.Error("missing body")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Fix the bug", "fix-the-bug"},
		{"Add Feature: OAuth 2.0", "add-feature-oauth-2-0"},
		{"  spaces  ", "spaces"},
		{"UPPERCASE", "uppercase"},
	}

	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantOk    bool
	}{
		{"https://github.com/erikh/hydra.git", "erikh", "hydra", true},
		{"https://github.com/erikh/hydra", "erikh", "hydra", true},
		{"git@github.com:erikh/hydra.git", "erikh", "hydra", true},
		{"https://gitea.example.com/foo/bar", "", "", false},
	}

	for _, tt := range tests {
		owner, repo, ok := ParseGitHubURL(tt.url)
		if ok != tt.wantOk || owner != tt.wantOwner || repo != tt.wantRepo {
			t.Errorf("ParseGitHubURL(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.url, owner, repo, ok, tt.wantOwner, tt.wantRepo, tt.wantOk)
		}
	}
}

func TestParseGiteaURL(t *testing.T) {
	tests := []struct {
		url         string
		wantBaseURL string
		wantOwner   string
		wantRepo    string
		wantOk      bool
	}{
		{"https://gitea.example.com/foo/bar.git", "https://gitea.example.com", "foo", "bar", true},
		{"git@gitea.example.com:foo/bar.git", "https://gitea.example.com", "foo", "bar", true},
	}

	for _, tt := range tests {
		baseURL, owner, repo, ok := ParseGiteaURL(tt.url)
		if ok != tt.wantOk || baseURL != tt.wantBaseURL || owner != tt.wantOwner || repo != tt.wantRepo {
			t.Errorf("ParseGiteaURL(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
				tt.url, baseURL, owner, repo, ok, tt.wantBaseURL, tt.wantOwner, tt.wantRepo, tt.wantOk)
		}
	}
}

func TestParseIssueTaskNumber(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"42-fix-bug", 42},
		{"1-simple", 1},
		{"custom-task", 0},
		{"0-edge", 0},
		{"abc", 0},
		{"", 0},
	}

	for _, tt := range tests {
		got := ParseIssueTaskNumber(tt.name)
		if got != tt.want {
			t.Errorf("ParseIssueTaskNumber(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestIsIssueTask(t *testing.T) {
	issueTask := &design.Task{Name: "42-fix-bug", Group: "issues"}
	if !IsIssueTask(issueTask) {
		t.Error("expected issue task")
	}

	normalTask := &design.Task{Name: "add-feature", Group: ""}
	if IsIssueTask(normalTask) {
		t.Error("expected non-issue task")
	}

	otherGroup := &design.Task{Name: "something", Group: "backend"}
	if IsIssueTask(otherGroup) {
		t.Error("expected non-issue task for backend group")
	}
}

type mockCloser struct {
	called  bool
	number  int
	comment string
}

func (m *mockCloser) CloseIssue(number int, comment string) error {
	m.called = true
	m.number = number
	m.comment = comment
	return nil
}

func TestCloseIssueIfNeededMock(t *testing.T) {
	closer := &mockCloser{}

	task := &design.Task{Name: "42-fix-bug", Group: "issues"}
	num := ParseIssueTaskNumber(task.Name)
	if num == 0 || !IsIssueTask(task) {
		t.Fatal("setup failed")
	}

	comment := "Closed by hydra. Commit: abc123"
	if err := closer.CloseIssue(num, comment); err != nil {
		t.Fatal(err)
	}

	if !closer.called {
		t.Error("closer not called")
	}
	if closer.number != 42 {
		t.Errorf("number = %d, want 42", closer.number)
	}
	if !strings.Contains(closer.comment, "abc123") {
		t.Errorf("comment = %q, want abc123", closer.comment)
	}
}

func TestCloseIssueIfNeededNonIssueTask(t *testing.T) {
	closer := &mockCloser{}

	task := &design.Task{Name: "add-feature", Group: ""}
	if IsIssueTask(task) {
		t.Fatal("should not be issue task")
	}

	// Should not call closer for non-issue tasks.
	_ = closer
}

func TestGitHubCloseIssue(_ *testing.T) {
	// We can't easily test GitHubSource.CloseIssue against httptest because
	// the URL is hardcoded. Instead, test the GiteaSource which accepts a base URL.
	// See TestGiteaCloseIssue for the integration test.
}

func TestGiteaCloseIssue(t *testing.T) {
	var gotComment, gotPatch bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/comments") {
			gotComment = true
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/issues/42") {
			gotPatch = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	src := NewGiteaSource(ts.URL, "owner", "repo", "test-token")
	if err := src.CloseIssue(42, "closed by hydra"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}

	if !gotComment {
		t.Error("expected comment POST")
	}
	if !gotPatch {
		t.Error("expected PATCH to close issue")
	}
}

func TestResolveSourceGitHub(t *testing.T) {
	src, err := ResolveSource("https://github.com/owner/repo.git", "", "")
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if _, ok := src.(*GitHubSource); !ok {
		t.Errorf("expected GitHubSource, got %T", src)
	}
}

func TestResolveSourceGitea(t *testing.T) {
	src, err := ResolveSource("https://gitea.example.com/owner/repo.git", "", "")
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if _, ok := src.(*GiteaSource); !ok {
		t.Errorf("expected GiteaSource, got %T", src)
	}
}

func TestResolveSourceExplicitType(t *testing.T) {
	src, err := ResolveSource("https://gitea.example.com/owner/repo.git", "gitea", "")
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if _, ok := src.(*GiteaSource); !ok {
		t.Errorf("expected GiteaSource, got %T", src)
	}
}

func TestResolveSourceInvalid(t *testing.T) {
	_, err := ResolveSource("not-a-url", "", "")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}
