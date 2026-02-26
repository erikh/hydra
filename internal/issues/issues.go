// Package issues imports open issues from GitHub or Gitea as design tasks.
package issues

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Issue represents a single issue from a remote source.
type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []string
	URL    string
}

// Source is the interface for fetching issues from a remote.
type Source interface {
	FetchOpenIssues(ctx context.Context, labels []string) ([]Issue, error)
}

// Sync imports open issues into the design directory under tasks/issues/.
func Sync(ctx context.Context, designDir string, source Source, labels []string) (created, skipped int, err error) {
	issues, err := source.FetchOpenIssues(ctx, labels)
	if err != nil {
		return 0, 0, fmt.Errorf("fetching issues: %w", err)
	}

	issuesDir := filepath.Join(designDir, "tasks", "issues")
	if err := os.MkdirAll(issuesDir, 0o750); err != nil {
		return 0, 0, fmt.Errorf("creating issues directory: %w", err)
	}

	// Create group.md if missing.
	groupPath := filepath.Join(issuesDir, "group.md")
	if _, err := os.Stat(groupPath); os.IsNotExist(err) {
		if err := os.WriteFile(groupPath, []byte("Imported from repository issues.\n"), 0o600); err != nil {
			return 0, 0, fmt.Errorf("creating group.md: %w", err)
		}
	}

	for _, issue := range issues {
		// Check if a file already exists for this issue number.
		if issueFileExists(issuesDir, issue.Number) {
			skipped++
			continue
		}

		filename := fmt.Sprintf("%d-%s.md", issue.Number, slugify(issue.Title))
		filePath := filepath.Join(issuesDir, filename)

		content := formatIssueContent(issue)
		if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
			return created, skipped, fmt.Errorf("writing issue %d: %w", issue.Number, err)
		}
		created++
	}

	return created, skipped, nil
}

// issueFileExists checks if any file in the directory starts with the issue number prefix.
func issueFileExists(dir string, number int) bool {
	prefix := fmt.Sprintf("%d-", number)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return true
		}
	}
	return false
}

// formatIssueContent formats an issue into the task file content.
func formatIssueContent(issue Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Issue #%d: %s\n", issue.Number, issue.Title)
	fmt.Fprintf(&b, "URL: %s\n", issue.URL)
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	b.WriteString("\n")
	b.WriteString(issue.Body)
	if !strings.HasSuffix(issue.Body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a title into a URL-friendly slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
		s = strings.TrimRight(s, "-")
	}
	return s
}
