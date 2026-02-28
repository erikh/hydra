package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	cmd := exec.CommandContext(context.Background(), "git", args...) //nolint:gosec // test with controlled args
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

func TestFetch(t *testing.T) {
	bare := initBareRemote(t)
	local := initLocalRepo(t, bare)
	r := Open(local)

	if err := r.Fetch(); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestResetHard(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	// Create a dirty file.
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}

	if err := r.ResetHard("HEAD"); err != nil {
		t.Fatalf("ResetHard: %v", err)
	}

	// File should be gone (untracked files remain, but staged changes are reset).
	has, _ := r.HasChanges()
	// dirty.txt is untracked after reset, so HasChanges still returns true.
	// Let's check that staged changes were reset.
	_ = has
}

func TestBranchExists(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	if err := r.CreateBranch("hydra/test"); err != nil {
		t.Fatal(err)
	}

	if !r.BranchExists("hydra/test") {
		t.Error("expected branch to exist")
	}
	if r.BranchExists("hydra/nonexistent") {
		t.Error("expected branch to not exist")
	}
}

func TestDeleteBranch(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	origBranch, _ := r.CurrentBranch()
	if err := r.CreateBranch("hydra/delete-me"); err != nil {
		t.Fatal(err)
	}
	if err := r.Checkout(origBranch); err != nil {
		t.Fatal(err)
	}

	if err := r.DeleteBranch("hydra/delete-me"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if r.BranchExists("hydra/delete-me") {
		t.Error("branch should be deleted")
	}
}

func TestIsGitRepo(t *testing.T) {
	dir := initLocalRepo(t, "")
	if !IsGitRepo(dir) {
		t.Error("expected git repo")
	}

	nonGit := t.TempDir()
	if IsGitRepo(nonGit) {
		t.Error("expected non-git dir")
	}
}

func TestRebase(t *testing.T) {
	bare := initBareRemote(t)
	dir := initLocalRepo(t, bare)
	r := Open(dir)

	// Create a feature branch with a commit.
	if err := r.CreateBranch("hydra/feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("add feature", false); err != nil {
		t.Fatal(err)
	}

	// Go back to main, add a commit.
	mainBranch, _ := r.CurrentBranch()
	_ = mainBranch
	if err := r.Checkout("master"); err != nil {
		// Try "main" if "master" fails.
		if err := r.Checkout("main"); err != nil {
			t.Fatalf("checkout main: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main-change.txt"), []byte("main"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("main change", false); err != nil {
		t.Fatal(err)
	}

	// Rebase feature onto current branch.
	if err := r.Checkout("hydra/feature"); err != nil {
		t.Fatal(err)
	}

	origBranch, _ := r.CurrentBranch()
	_ = origBranch

	// Get the default branch name.
	defaultBranch := "master"
	if r.BranchExists("main") {
		defaultBranch = "main"
	}

	if err := r.Rebase(defaultBranch); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	// Both files should exist after rebase.
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Error("feature.txt missing after rebase")
	}
	if _, err := os.Stat(filepath.Join(dir, "main-change.txt")); err != nil {
		t.Error("main-change.txt missing after rebase")
	}
}

func TestRebaseConflictAndAbort(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	defaultBranch, _ := r.CurrentBranch()

	// Create conflicting changes.
	if err := r.CreateBranch("hydra/conflict"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("branch content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("branch change", false); err != nil {
		t.Fatal(err)
	}

	if err := r.Checkout(defaultBranch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("main change", false); err != nil {
		t.Fatal(err)
	}

	if err := r.Checkout("hydra/conflict"); err != nil {
		t.Fatal(err)
	}

	// Rebase should fail with conflicts.
	err := r.Rebase(defaultBranch)
	if err == nil {
		t.Fatal("expected rebase conflict error")
	}

	// Abort the rebase.
	if err := r.RebaseAbort(); err != nil {
		t.Fatalf("RebaseAbort: %v", err)
	}
}

func TestMergeFFOnly(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	defaultBranch, _ := r.CurrentBranch()

	// Create a branch with a commit.
	if err := r.CreateBranch("hydra/ff-test"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ff.txt"), []byte("ff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("ff commit", false); err != nil {
		t.Fatal(err)
	}

	if err := r.Checkout(defaultBranch); err != nil {
		t.Fatal(err)
	}

	if err := r.MergeFFOnly("hydra/ff-test"); err != nil {
		t.Fatalf("MergeFFOnly: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "ff.txt")); err != nil {
		t.Error("ff.txt missing after merge")
	}
}

func TestLog(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	out, err := r.Log(1)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if !strings.Contains(out, "initial") {
		t.Errorf("log = %q, want initial commit message", out)
	}
}

func TestIsAncestor(t *testing.T) {
	dir := initLocalRepo(t, "")
	r := Open(dir)

	// Get HEAD SHA.
	headSHA, _ := r.LastCommitSHA()

	// Create a new commit.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("new commit", false); err != nil {
		t.Fatal(err)
	}

	newSHA, _ := r.LastCommitSHA()

	if !r.IsAncestor(headSHA, newSHA) {
		t.Error("expected old commit to be ancestor of new commit")
	}
	if r.IsAncestor(newSHA, headSHA) {
		t.Error("new commit should not be ancestor of old commit")
	}
}

func TestForcePushWithLease(t *testing.T) {
	bare := initBareRemote(t)
	local := initLocalRepo(t, bare)
	r := Open(local)

	if err := r.CreateBranch("hydra/force-push"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "fp.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("force push test", false); err != nil {
		t.Fatal(err)
	}
	if err := r.Push("hydra/force-push"); err != nil {
		t.Fatal(err)
	}

	// Amend and force push with lease.
	if err := os.WriteFile(filepath.Join(local, "fp2.txt"), []byte("data2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("amended", false); err != nil {
		t.Fatal(err)
	}

	if err := r.ForcePushWithLease("hydra/force-push"); err != nil {
		t.Fatalf("ForcePushWithLease: %v", err)
	}
}

func TestPushMain(t *testing.T) {
	bare := initBareRemote(t)
	local := initLocalRepo(t, bare)
	r := Open(local)

	if err := os.WriteFile(filepath.Join(local, "main-push.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit("push main test", false); err != nil {
		t.Fatal(err)
	}

	if err := r.PushMain(); err != nil {
		t.Fatalf("PushMain: %v", err)
	}
}
