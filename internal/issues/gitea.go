package issues

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// GiteaSource fetches issues from a Gitea instance.
type GiteaSource struct {
	BaseURL string // e.g. "https://gitea.example.com"
	Owner   string
	Repo    string
	Token   string // from GITEA_TOKEN or hydra.yml
}

// NewGiteaSource creates a GiteaSource.
func NewGiteaSource(baseURL, owner, repo, token string) *GiteaSource {
	if token == "" {
		token = os.Getenv("GITEA_TOKEN")
	}
	return &GiteaSource{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Owner:   owner,
		Repo:    repo,
		Token:   token,
	}
}

type giteaIssue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Labels  []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// FetchOpenIssues retrieves open issues from a Gitea instance.
func (g *GiteaSource) FetchOpenIssues(ctx context.Context, labels []string) ([]Issue, error) {
	apiURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues?state=open&type=issues&limit=50",
		g.BaseURL, g.Owner, g.Repo)
	if len(labels) > 0 {
		apiURL += "&labels=" + url.QueryEscape(strings.Join(labels, ","))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	if g.Token != "" {
		req.Header.Set("Authorization", "token "+g.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitea API returned status %d", resp.StatusCode)
	}

	var gtIssues []giteaIssue
	if err := json.NewDecoder(resp.Body).Decode(&gtIssues); err != nil {
		return nil, fmt.Errorf("decoding Gitea response: %w", err)
	}

	var result []Issue
	for _, gi := range gtIssues {
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

// CloseIssue closes a Gitea issue with an optional comment.
func (g *GiteaSource) CloseIssue(number int, comment string) error {
	ctx := context.Background()

	// Post comment if provided.
	if comment != "" {
		commentURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d/comments", g.BaseURL, g.Owner, g.Repo, number)
		body := fmt.Sprintf(`{"body":%q}`, comment)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, commentURL, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if g.Token != "" {
			req.Header.Set("Authorization", "token "+g.Token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("posting comment: %w", err)
		}
		_ = resp.Body.Close()
	}

	// Close the issue.
	closeURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d", g.BaseURL, g.Owner, g.Repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, closeURL, strings.NewReader(`{"state":"closed"}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.Token != "" {
		req.Header.Set("Authorization", "token "+g.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("closing issue: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("gitea API returned status %d when closing issue #%d", resp.StatusCode, number)
	}

	return nil
}

// ParseGiteaURL extracts the base URL, owner, and repo from a non-GitHub remote URL.
// Supports https://host/owner/repo and git@host:owner/repo formats.
func ParseGiteaURL(remoteURL string) (baseURL, owner, repo string, ok bool) {
	// SSH format: git@host:owner/repo.git
	if rest, found := strings.CutPrefix(remoteURL, "git@"); found {
		host, path, ok := strings.Cut(rest, ":")
		if !ok {
			return "", "", "", false
		}
		owner, repo, ok := parseOwnerRepo(path)
		if !ok {
			return "", "", "", false
		}
		return "https://" + host, owner, repo, true
	}

	// HTTPS format.
	u, err := url.Parse(remoteURL)
	if err != nil || u.Host == "" {
		return "", "", "", false
	}
	owner, repo, ok = parseOwnerRepo(u.Path)
	if !ok {
		return "", "", "", false
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), owner, repo, true
}
