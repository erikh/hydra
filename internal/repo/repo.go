// Package repo provides git operations via os/exec.
package repo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo represents a local git repository.
type Repo struct {
	Dir string
}

// Clone clones a git repository from url into dest.
func Clone(url, dest string) (*Repo, error) {
	cmd := exec.CommandContext(context.Background(), "git", "clone", url, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git clone: %w\n%s", err, out)
	}
	return &Repo{Dir: dest}, nil
}

// Open returns a Repo handle for an existing directory.
func Open(dir string) *Repo {
	return &Repo{Dir: dir}
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", args[0], err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// CreateBranch creates and checks out a new branch.
func (r *Repo) CreateBranch(name string) error {
	_, err := r.run("checkout", "-b", name)
	return err
}

// Checkout switches to an existing branch.
func (r *Repo) Checkout(name string) error {
	_, err := r.run("checkout", name)
	return err
}

// AddAll stages all changes.
func (r *Repo) AddAll() error {
	_, err := r.run("add", "-A")
	return err
}

// Commit creates a commit with the given message. If sign is true, the commit is GPG-signed.
func (r *Repo) Commit(message string, sign bool) error {
	args := []string{"commit", "-m", message}
	if sign {
		args = append(args, "-S")
	}
	_, err := r.run(args...)
	return err
}

// Push pushes the given branch to origin.
func (r *Repo) Push(branch string) error {
	_, err := r.run("push", "-u", "origin", branch)
	return err
}

// HasChanges returns true if the working tree has uncommitted changes.
func (r *Repo) HasChanges() (bool, error) {
	out, err := r.run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// HasSigningKey returns true if the repo has a GPG signing key configured.
func (r *Repo) HasSigningKey() bool {
	out, _ := r.run("config", "user.signingkey")
	return out != ""
}

// CurrentBranch returns the name of the currently checked out branch.
func (r *Repo) CurrentBranch() (string, error) {
	return r.run("rev-parse", "--abbrev-ref", "HEAD")
}

// LastCommitSHA returns the full SHA of the HEAD commit.
func (r *Repo) LastCommitSHA() (string, error) {
	return r.run("rev-parse", "HEAD")
}

// Fetch runs git fetch origin.
func (r *Repo) Fetch() error {
	_, err := r.run("fetch", "origin")
	return err
}

// ResetHard runs git reset --hard to the given ref.
func (r *Repo) ResetHard(ref string) error {
	_, err := r.run("reset", "--hard", ref)
	return err
}

// BranchExists returns true if the named branch exists locally.
func (r *Repo) BranchExists(name string) bool {
	_, err := r.run("rev-parse", "--verify", name)
	return err == nil
}

// DeleteBranch deletes a local branch.
func (r *Repo) DeleteBranch(name string) error {
	_, err := r.run("branch", "-D", name)
	return err
}

// IsGitRepo returns true if dir contains a .git directory or file.
func IsGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Clean removes untracked files and directories.
func (r *Repo) Clean() error {
	_, err := r.run("clean", "-fd")
	return err
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
		// During a rebase with conflicts, status still works.
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
	_, err := r.run("push", "--force-with-lease", "origin", branch)
	return err
}

// MergeFFOnly merges the given branch using fast-forward only.
func (r *Repo) MergeFFOnly(branch string) error {
	_, err := r.run("merge", "--ff-only", branch)
	return err
}

// PushMain pushes the main branch to origin.
func (r *Repo) PushMain() error {
	_, err := r.run("push", "origin", "HEAD")
	return err
}

// Log returns the last n commit messages in oneline format.
func (r *Repo) Log(n int) (string, error) {
	return r.run("log", "--oneline", fmt.Sprintf("-%d", n))
}

// IsAncestor returns true if ancestor is an ancestor of ref.
func (r *Repo) IsAncestor(ancestor, ref string) bool {
	_, err := r.run("merge-base", "--is-ancestor", ancestor, ref)
	return err == nil
}
