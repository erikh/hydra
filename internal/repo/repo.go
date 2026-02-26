package repo

import (
	"fmt"
	"os/exec"
	"strings"
)

type Repo struct {
	Dir string
}

func Clone(url, dest string) (*Repo, error) {
	cmd := exec.Command("git", "clone", url, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git clone: %s\n%s", err, out)
	}
	return &Repo{Dir: dest}, nil
}

func Open(dir string) *Repo {
	return &Repo{Dir: dir}
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s\n%s", args[0], err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Repo) CreateBranch(name string) error {
	_, err := r.run("checkout", "-b", name)
	return err
}

func (r *Repo) Checkout(name string) error {
	_, err := r.run("checkout", name)
	return err
}

func (r *Repo) AddAll() error {
	_, err := r.run("add", "-A")
	return err
}

func (r *Repo) Commit(message string, sign bool) error {
	args := []string{"commit", "-m", message}
	if sign {
		args = append(args, "-S")
	}
	_, err := r.run(args...)
	return err
}

func (r *Repo) Push(branch string) error {
	_, err := r.run("push", "-u", "origin", branch)
	return err
}

func (r *Repo) HasChanges() (bool, error) {
	out, err := r.run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func (r *Repo) HasSigningKey() bool {
	out, _ := r.run("config", "user.signingkey")
	return out != ""
}

func (r *Repo) CurrentBranch() (string, error) {
	return r.run("rev-parse", "--abbrev-ref", "HEAD")
}
