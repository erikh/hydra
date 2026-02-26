package issues

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// GitHubSource fetches issues from the GitHub REST API.
type GitHubSource struct {
	Owner string
	Repo  string
	Token string // optional; from GITHUB_TOKEN
}

// NewGitHubSource creates a GitHubSource from an owner/repo pair.
func NewGitHubSource(owner, repo string) *GitHubSource {
	return &GitHubSource{
		Owner: owner,
		Repo:  repo,
		Token: os.Getenv("GITHUB_TOKEN"),
	}
}

type githubIssue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Labels  []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *struct{} `json:"pull_request"` // non-nil means it's a PR
}

// FetchOpenIssues retrieves open issues from GitHub.
func (g *GitHubSource) FetchOpenIssues(ctx context.Context, labels []string) ([]Issue, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?state=open&per_page=100", g.Owner, g.Repo)
	if len(labels) > 0 {
		url += "&labels=" + strings.Join(labels, ",")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var ghIssues []githubIssue
	if err := json.NewDecoder(resp.Body).Decode(&ghIssues); err != nil {
		return nil, fmt.Errorf("decoding GitHub response: %w", err)
	}

	var result []Issue
	for _, gi := range ghIssues {
		// Skip pull requests (GitHub includes them in the issues endpoint).
		if gi.PullRequest != nil {
			continue
		}
		var labelNames []string
		for _, l := range gi.Labels {
			labelNames = append(labelNames, l.Name)
		}
		result = append(result, Issue{
			Number: gi.Number,
			Title:  gi.Title,
			Body:   gi.Body,
			Labels: labelNames,
			URL:    gi.HTMLURL,
		})
	}

	return result, nil
}

// ParseGitHubURL extracts owner and repo from a GitHub URL.
// Supports https://github.com/owner/repo and git@github.com:owner/repo formats.
func ParseGitHubURL(remoteURL string) (owner, repo string, ok bool) {
	// HTTPS format.
	if strings.Contains(remoteURL, "github.com/") {
		parts := strings.Split(remoteURL, "github.com/")
		if len(parts) != 2 {
			return "", "", false
		}
		return parseOwnerRepo(parts[1])
	}

	// SSH format.
	if strings.Contains(remoteURL, "github.com:") {
		parts := strings.Split(remoteURL, "github.com:")
		if len(parts) != 2 {
			return "", "", false
		}
		return parseOwnerRepo(parts[1])
	}

	return "", "", false
}

func parseOwnerRepo(path string) (string, string, bool) {
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
