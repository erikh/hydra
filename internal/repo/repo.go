// Package repo provides git operations via os/exec.
package repo

import (
	"context"
	"fmt"
	"os/exec"
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
