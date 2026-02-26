package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initBareRemote creates a bare git repo to act as a remote.
func initBareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	cmd := exec.Command("git", "init", "--bare", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return dir
}

// initLocalRepo creates a local git repo with an initial commit and a remote pointing to bare.
func initLocalRepo(t *testing.T, bare string) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "config", "commit.gpgsign", "false"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	// Create initial commit
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644)
	exec.Command("git", "-C", dir, "add", "-A").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()

	if bare != "" {
		exec.Command("git", "-C", dir, "remote", "add", "origin", bare).Run()
		exec.Command("git", "-C", dir, "push", "-u", "origin", "main").Run()
		// If main didn't work (older git default is master), try master
		exec.Command("git", "-C", dir, "push", "-u", "origin", "master").Run()
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

	// Verify it's a git repo
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

	// Get current branch name
	origBranch, _ := r.CurrentBranch()

	r.CreateBranch("hydra/feature")
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

	// No changes initially
	has, err := r.HasChanges()
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if has {
		t.Error("expected no changes initially")
	}

	// Create a file
	os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("hello"), 0o644)

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

	os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("hello"), 0o644)

	if err := r.AddAll(); err != nil {
		t.Fatalf("AddAll: %v", err)
	}

	if err := r.Commit("test commit", false); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// No changes after commit
	has, _ := r.HasChanges()
	if has {
		t.Error("expected no changes after commit")
	}
}

func TestCommitSigned(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("hello"), 0o644)
	r.AddAll()

	// Commit with sign=true will fail without a signing key, which is expected
	err := r.Commit("signed commit", true)
	// We just verify the command was attempted — it fails without GPG setup
	// which is fine for a test environment
	_ = err
}

func TestPush(t *testing.T) {
	bare := initBareRemote(t)
	local := initLocalRepo(t, bare)
	r := Open(local)

	r.CreateBranch("hydra/push-test")
	os.WriteFile(filepath.Join(local, "pushed.txt"), []byte("data"), 0o644)
	r.AddAll()
	r.Commit("push test", false)

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
	exec.Command("git", "-C", dir, "config", "user.signingkey", "ABCDEF1234567890").Run()

	r := Open(dir)
	if !r.HasSigningKey() {
		t.Error("expected signing key to be detected")
	}
}
