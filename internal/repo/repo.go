// Package repo provides git operations via go-git with shell-out fallback.
package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Repo represents a local git repository.
type Repo struct {
	Dir      string
	repo     *git.Repository
	auth     transport.AuthMethod
	authDone bool
}

// Clone clones a git repository from url into dest.
func Clone(url, dest string) (*Repo, error) {
	r, err := git.PlainClone(dest, false, &git.CloneOptions{
		URL:  url,
		Auth: detectAuthFromURL(url),
	})
	if err != nil {
		return nil, fmt.Errorf("git clone: %w", err)
	}
	return &Repo{Dir: dest, repo: r}, nil
}

// Open returns a Repo handle for an existing directory.
// If the directory is not a valid git repo, the internal repo handle is left nil
// and will be lazily opened by ensure().
func Open(dir string) *Repo {
	r, err := git.PlainOpen(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open git repo at %s: %v\n", dir, err)
	}
	return &Repo{Dir: dir, repo: r}
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...) //nolint:gosec // args are controlled internally
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", args[0], err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// ensure lazily opens the go-git repository if not already set.
func (r *Repo) ensure() error {
	if r.repo != nil {
		return nil
	}
	repo, err := git.PlainOpen(r.Dir)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	r.repo = repo
	return nil
}

// isSSHURL returns true if the URL uses an SSH transport.
func isSSHURL(url string) bool {
	return strings.HasPrefix(url, "git@") || strings.HasPrefix(url, "ssh://")
}

// resolveAuth lazily detects the remote URL scheme and sets auth for SSH remotes.
func (r *Repo) resolveAuth() {
	if r.authDone {
		return
	}
	r.authDone = true
	url, err := r.RemoteURL()
	if err != nil {
		return
	}
	if isSSHURL(url) {
		auth, err := gitssh.NewSSHAgentAuth("git")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not set up SSH agent auth: %v\n", err)
			return
		}
		r.auth = auth
	}
}

// detectAuthFromURL returns SSH agent auth if the URL is an SSH remote.
func detectAuthFromURL(url string) transport.AuthMethod {
	if isSSHURL(url) {
		auth, err := gitssh.NewSSHAgentAuth("git")
		if err != nil {
			return nil
		}
		return auth
	}
	return nil
}

// isHTTPS returns true if the origin remote uses HTTPS.
func (r *Repo) isHTTPS() bool {
	url, err := r.RemoteURL()
	if err != nil {
		return false
	}
	return strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
}

// commitIdentity returns the user name and email from repo config,
// falling back to global config.
func (r *Repo) commitIdentity() (name, email string) {
	localCfg, err := r.repo.ConfigScoped(config.LocalScope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read local git config: %v\n", err)
	}
	globalCfg, err := r.repo.ConfigScoped(config.GlobalScope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read global git config: %v\n", err)
	}

	if localCfg != nil {
		name = localCfg.User.Name
		email = localCfg.User.Email
	}
	if globalCfg != nil {
		if name == "" {
			name = globalCfg.User.Name
		}
		if email == "" {
			email = globalCfg.User.Email
		}
	}
	return name, email
}

// CreateBranch creates and checks out a new branch.
func (r *Repo) CreateBranch(name string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	head, err := r.repo.Head()
	if err != nil {
		return fmt.Errorf("head: %w", err)
	}
	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
		Hash:   head.Hash(),
		Create: true,
	})
}

// Checkout switches to an existing branch.
func (r *Repo) Checkout(name string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
	})
}

// AddAll stages all changes.
func (r *Repo) AddAll() error {
	if err := r.ensure(); err != nil {
		return err
	}
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	_, err = w.Add(".")
	return err
}

// Commit creates a commit with the given message. If sign is true, the commit
// is GPG-signed via the git CLI (go-git has no signing support).
func (r *Repo) Commit(message string, sign bool) error {
	if sign {
		args := []string{"commit", "-m", message, "-S"}
		_, err := r.run(args...)
		return err
	}
	if err := r.ensure(); err != nil {
		return err
	}
	name, email := r.commitIdentity()
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	_, err = w.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  name,
			Email: email,
			When:  time.Now(),
		},
	})
	return err
}

// Push pushes the given branch to origin.
func (r *Repo) Push(branch string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	r.resolveAuth()
	if r.isHTTPS() {
		_, err := r.run("push", "origin", branch)
		return err
	}
	refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
	err := r.repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refSpec},
		Auth:       r.auth,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// HasChanges returns true if the working tree has uncommitted changes.
func (r *Repo) HasChanges() (bool, error) {
	if err := r.ensure(); err != nil {
		return false, err
	}
	w, err := r.repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("worktree: %w", err)
	}
	status, err := w.Status()
	if err != nil {
		return false, err
	}
	return !status.IsClean(), nil
}

// HasSigningKey returns true if the repo has a GPG signing key configured.
func (r *Repo) HasSigningKey() bool {
	if err := r.ensure(); err != nil {
		return false
	}
	cfg, err := r.repo.ConfigScoped(config.LocalScope)
	if err != nil {
		return false
	}
	// go-git's config doesn't expose user.signingkey directly,
	// so fall back to raw section access.
	for _, sec := range cfg.Raw.Sections {
		if sec.Name == "user" {
			for _, opt := range sec.Options {
				if opt.Key == "signingkey" && opt.Value != "" {
					return true
				}
			}
		}
	}
	return false
}

// CurrentBranch returns the name of the currently checked out branch.
func (r *Repo) CurrentBranch() (string, error) {
	if err := r.ensure(); err != nil {
		return "", err
	}
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("head: %w", err)
	}
	return head.Name().Short(), nil
}

// LastCommitSHA returns the full SHA of the HEAD commit.
func (r *Repo) LastCommitSHA() (string, error) {
	if err := r.ensure(); err != nil {
		return "", err
	}
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("head: %w", err)
	}
	return head.Hash().String(), nil
}

// Fetch runs git fetch origin.
func (r *Repo) Fetch() error {
	if err := r.ensure(); err != nil {
		return err
	}
	r.resolveAuth()
	if r.isHTTPS() {
		_, err := r.run("fetch", "origin")
		return err
	}
	err := r.repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Auth:       r.auth,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// ResetHard runs git reset --hard to the given ref.
func (r *Repo) ResetHard(ref string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return fmt.Errorf("resolve %q: %w", ref, err)
	}
	return w.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: *hash,
	})
}

// BranchExists returns true if the named ref exists. It checks local branches
// first, then remote tracking refs (e.g. "origin/main" resolves to
// refs/remotes/origin/main), and finally falls back to rev-parse style
// resolution via go-git.
func (r *Repo) BranchExists(name string) bool {
	if err := r.ensure(); err != nil {
		return false
	}
	// Check local branch.
	if _, err := r.repo.Reference(plumbing.NewBranchReferenceName(name), false); err == nil {
		return true
	}
	// Check remote tracking ref (e.g. "origin/main" -> refs/remotes/origin/main).
	remoteRef := plumbing.ReferenceName("refs/remotes/" + name)
	if _, err := r.repo.Reference(remoteRef, false); err == nil {
		return true
	}
	// Last resort: try resolving as a revision.
	_, err := r.repo.ResolveRevision(plumbing.Revision(name))
	return err == nil
}

// DeleteBranch deletes a local branch.
func (r *Repo) DeleteBranch(name string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	return r.repo.Storer.RemoveReference(plumbing.NewBranchReferenceName(name))
}

// DeleteRemoteBranch deletes a branch from the origin remote.
func (r *Repo) DeleteRemoteBranch(name string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	r.resolveAuth()
	if r.isHTTPS() {
		_, err := r.run("push", "origin", "--delete", name)
		return err
	}
	refSpec := config.RefSpec(":refs/heads/" + name)
	err := r.repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refSpec},
		Auth:       r.auth,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// IsGitRepo returns true if dir contains a .git directory or file.
func IsGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// resolveCommit resolves a ref string to a Commit object.
func (r *Repo) resolveCommit(ref string) (*object.Commit, error) {
	if err := r.ensure(); err != nil {
		return nil, err
	}
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", ref, err)
	}
	return r.repo.CommitObject(*hash)
}

// Clean removes untracked files and directories.
func (r *Repo) Clean() error {
	if err := r.ensure(); err != nil {
		return err
	}
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	return w.Clean(&git.CleanOptions{Dir: true})
}

// Rebase runs git rebase onto the given ref.
func (r *Repo) Rebase(onto string) error {
	_, err := r.run("rebase", onto)
	return err
}

// RebaseContinue runs git rebase --continue.
func (r *Repo) RebaseContinue() error {
	_, err := r.run("rebase", "--continue")
	return err
}

// RebaseAbort runs git rebase --abort.
func (r *Repo) RebaseAbort() error {
	_, err := r.run("rebase", "--abort")
	return err
}

// HasConflicts returns true if there are unmerged paths.
func (r *Repo) HasConflicts() (bool, error) {
	out, err := r.run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	for line := range strings.SplitSeq(out, "\n") {
		if len(line) >= 2 && (line[0] == 'U' || line[1] == 'U' ||
			(line[0] == 'A' && line[1] == 'A') ||
			(line[0] == 'D' && line[1] == 'D')) {
			return true, nil
		}
	}
	return false, nil
}

// ConflictFiles returns the list of files with merge conflicts.
func (r *Repo) ConflictFiles() ([]string, error) {
	out, err := r.run("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// ForcePushWithLease pushes the given branch with --force-with-lease.
func (r *Repo) ForcePushWithLease(branch string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	r.resolveAuth()
	if r.isHTTPS() {
		_, err := r.run("push", "--force-with-lease", "origin", branch)
		return err
	}
	refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
	err := r.repo.Push(&git.PushOptions{
		RemoteName:     "origin",
		RefSpecs:       []config.RefSpec{refSpec},
		ForceWithLease: &git.ForceWithLease{},
		Auth:           r.auth,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// MergeFFOnly merges the given branch using fast-forward only.
func (r *Repo) MergeFFOnly(branch string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	ref, err := r.repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return fmt.Errorf("resolve branch %q: %w", branch, err)
	}
	if err := r.repo.Merge(*ref, git.MergeOptions{Strategy: git.FastForwardMerge}); err != nil {
		return fmt.Errorf("merge --ff-only: %w", err)
	}
	// Merge only updates HEAD ref; reset worktree to match.
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	return w.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: ref.Hash(),
	})
}

// PushMain pushes the main branch to origin.
func (r *Repo) PushMain() error {
	if err := r.ensure(); err != nil {
		return err
	}
	r.resolveAuth()
	if r.isHTTPS() {
		_, err := r.run("push", "origin")
		return err
	}
	err := r.repo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth:       r.auth,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// Log returns the last n commit messages in oneline format.
func (r *Repo) Log(n int) (string, error) {
	if err := r.ensure(); err != nil {
		return "", err
	}
	iter, err := r.repo.Log(&git.LogOptions{})
	if err != nil {
		return "", fmt.Errorf("log: %w", err)
	}
	var lines []string
	count := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if count >= n {
			return storer.ErrStop
		}
		short := c.Hash.String()[:7]
		msg := strings.SplitN(c.Message, "\n", 2)[0]
		lines = append(lines, short+" "+msg)
		count++
		return nil
	})
	if err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

// IsAncestor returns true if ancestor is an ancestor of ref.
func (r *Repo) IsAncestor(ancestor, ref string) bool {
	ancestorCommit, err := r.resolveCommit(ancestor)
	if err != nil {
		return false
	}
	refCommit, err := r.resolveCommit(ref)
	if err != nil {
		return false
	}
	isAnc, err := ancestorCommit.IsAncestor(refCommit)
	if err != nil {
		return false
	}
	return isAnc
}

// RemoteURL returns the URL of the origin remote.
func (r *Repo) RemoteURL() (string, error) {
	if err := r.ensure(); err != nil {
		return "", err
	}
	remote, err := r.repo.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("remote origin: %w", err)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", errors.New("remote origin has no URLs")
	}
	return urls[0], nil
}

// MergeBase returns the merge-base commit between two refs.
func (r *Repo) MergeBase(a, b string) (string, error) {
	commitA, err := r.resolveCommit(a)
	if err != nil {
		return "", err
	}
	commitB, err := r.resolveCommit(b)
	if err != nil {
		return "", err
	}
	bases, err := commitA.MergeBase(commitB)
	if err != nil {
		return "", fmt.Errorf("merge-base: %w", err)
	}
	if len(bases) == 0 {
		return "", errors.New("no merge base found")
	}
	return bases[0].Hash.String(), nil
}

// DiffRange returns the diff between the merge-base of base..head and head.
func (r *Repo) DiffRange(base, head string) (string, error) {
	baseCommit, err := r.resolveCommit(base)
	if err != nil {
		return "", err
	}
	headCommit, err := r.resolveCommit(head)
	if err != nil {
		return "", err
	}
	bases, err := baseCommit.MergeBase(headCommit)
	if err != nil {
		return "", fmt.Errorf("merge-base: %w", err)
	}
	if len(bases) == 0 {
		return "", errors.New("no merge base found")
	}
	patch, err := bases[0].Patch(headCommit)
	if err != nil {
		return "", fmt.Errorf("patch: %w", err)
	}
	return patch.String(), nil
}
