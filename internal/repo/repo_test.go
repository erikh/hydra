package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initBareRemote creates a bare git repo to act as a remote.
func initBareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "init", "--bare", dir)
	return dir
}

func gitRun(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initLocalRepo creates a local git repo with an initial commit and a remote pointing to bare.
func initLocalRepo(t *testing.T, bare string) string {
	t.Helper()
	dir := t.TempDir()

	gitRun(t, "init", dir)
	gitRun(t, "-C", dir, "config", "user.email", "test@test.com")
	gitRun(t, "-C", dir, "config", "user.name", "Test")
	gitRun(t, "-C", dir, "config", "commit.gpgsign", "false")

	// Create initial commit.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", dir, "add", "-A")
	gitRun(t, "-C", dir, "commit", "-m", "initial")

	if bare != "" {
		gitRun(t, "-C", dir, "remote", "add", "origin", bare)
		// Push whatever the default branch is.
		out, err := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output() //nolint:gosec // test with controlled args
		if err != nil {
			t.Fatalf("getting branch: %v", err)
		}
		branch := string(out[:len(out)-1]) // trim newline
		gitRun(t, "-C", dir, "push", "-u", "origin", branch)
	}

	return dir
}

func TestClone(t *testing.T) {
	bare := initBareRemote(t)
	_ = initLocalRepo(t, bare)

	dest := filepath.Join(t.TempDir(), "clone")
	r, err := Clone(bare, dest)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if r.Dir != dest {
		t.Errorf("Dir = %q, want %q", r.Dir, dest)
	}

	// Verify it's a git repo.
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Error(".git directory not found in clone")
	}
}

func TestCloneInvalidURL(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "clone")
	_, err := Clone("file:///nonexistent/repo.git", dest)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestOpen(t *testing.T) {
	r := Open("/some/path")
	if r.Dir != "/some/path" {
		t.Errorf("Dir = %q", r.Dir)
	}
}

func TestCreateBranchAndCurrentBranch(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	if err := r.CreateBranch("hydra/test-task"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "hydra/test-task" {
		t.Errorf("CurrentBranch = %q, want hydra/test-task", branch)
	}
}

func TestCheckout(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	// Get current branch name.
	origBranch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}

	if err := r.CreateBranch("hydra/feature"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout(origBranch); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	branch, _ := r.CurrentBranch()
	if branch != origBranch {
		t.Errorf("CurrentBranch = %q, want %q", branch, origBranch)
	}
}

func TestHasChanges(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	// No changes initially.
	has, err := r.HasChanges()
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if has {
		t.Error("expected no changes initially")
	}

	// Create a file.
	if err := os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	has, err = r.HasChanges()
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if !has {
		t.Error("expected changes after creating file")
	}
}

func TestAddAllAndCommit(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	if err := os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := r.AddAll(); err != nil {
		t.Fatalf("AddAll: %v", err)
	}

	if err := r.Commit("test commit", false); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// No changes after commit.
	has, _ := r.HasChanges()
	if has {
		t.Error("expected no changes after commit")
	}
}

func TestCommitSigned(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	if err := os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}

	// Commit with sign=true will fail without a signing key, which is expected.
	_ = r.Commit("signed commit", true)
}

func TestPush(t *testing.T) {
	bare := initBareRemote(t)
	local := initLocalRepo(t, bare)
	r := Open(local)

	if err := r.CreateBranch("hydra/push-test"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "pushed.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("push test", false); err != nil {
		t.Fatal(err)
	}

	if err := r.Push("hydra/push-test"); err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestHasSigningKeyFalse(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	if r.HasSigningKey() {
		t.Error("expected no signing key in test repo")
	}
}

func TestHasSigningKeyTrue(t *testing.T) {
	dir := initLocalRepo(t, "")
	gitRun(t, "-C", dir, "config", "user.signingkey", "ABCDEF1234567890")

	r := Open(dir)
	if !r.HasSigningKey() {
		t.Error("expected signing key to be detected")
	}
}
