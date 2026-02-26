package issues

import (
	"fmt"
	"strings"
)

// ResolveSource determines the issue source from a repo URL and optional overrides.
func ResolveSource(repoURL, apiType, giteaURL string) (Source, error) {
	giteaToken := ""

	// Explicit api_type override.
	if apiType == "github" {
		owner, repo, ok := ParseGitHubURL(repoURL)
		if !ok {
			return nil, fmt.Errorf("cannot parse GitHub owner/repo from %q", repoURL)
		}
		return NewGitHubSource(owner, repo), nil
	}
	if apiType == "gitea" {
		baseURL := giteaURL
		if baseURL == "" {
			var owner, repo string
			var ok bool
			baseURL, owner, repo, ok = ParseGiteaURL(repoURL)
			if !ok {
				return nil, fmt.Errorf("cannot parse Gitea URL from %q", repoURL)
			}
			return NewGiteaSource(baseURL, owner, repo, giteaToken), nil
		}
		// Parse owner/repo from URL even when base URL is overridden.
		_, owner, repo, ok := ParseGiteaURL(repoURL)
		if !ok {
			return nil, fmt.Errorf("cannot parse owner/repo from %q", repoURL)
		}
		return NewGiteaSource(baseURL, owner, repo, giteaToken), nil
	}

	// Auto-detect: if URL contains github.com, use GitHub.
	if strings.Contains(repoURL, "github.com") {
		owner, repo, ok := ParseGitHubURL(repoURL)
		if !ok {
			return nil, fmt.Errorf("cannot parse GitHub owner/repo from %q", repoURL)
		}
		return NewGitHubSource(owner, repo), nil
	}

	// Default to Gitea for non-GitHub hosts.
	baseURL, owner, repo, ok := ParseGiteaURL(repoURL)
	if !ok {
		return nil, fmt.Errorf("cannot determine issue source from %q; set api_type in hydra.yml", repoURL)
	}
	return NewGiteaSource(baseURL, owner, repo, giteaToken), nil
}

// ResolveCloser resolves a Closer from the source, if the source implements it.
func ResolveCloser(source Source) Closer {
	if closer, ok := source.(Closer); ok {
		return closer
	}
	return nil
}
