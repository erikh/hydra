package runner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/erikh/hydra/internal/config"
	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/lock"
	"github.com/erikh/hydra/internal/repo"
)

// Fix scans the project for issues and fixes what it can.
// Returns an error only if scanning itself fails, not for individual issues.
func (r *Runner) Fix() error {
	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}

	var issues int

	i, err := r.fixDuplicateTaskNames()
	if err != nil {
		return fmt.Errorf("checking duplicate tasks: %w", err)
	}
	issues += i

	i, err = r.fixStaleLocks(baseDir)
	if err != nil {
		return fmt.Errorf("checking stale locks: %w", err)
	}
	issues += i

	i, err = r.fixWorkDirBranches(baseDir)
	if err != nil {
		return fmt.Errorf("checking work directories: %w", err)
	}
	issues += i

	i, err = r.fixWorkDirRemotes(baseDir)
	if err != nil {
		return fmt.Errorf("checking remotes: %w", err)
	}
	issues += i

	i = r.fixMissingStateDirs()
	issues += i

	i, err = r.fixOrphanedWorkDirs(baseDir)
	if err != nil {
		return fmt.Errorf("checking orphaned work dirs: %w", err)
	}
	issues += i

	if issues == 0 {
		fmt.Println("No issues found.")
	} else {
		fmt.Printf("\n%d issue(s) found.\n", issues)
	}

	return nil
}

// fixDuplicateTaskNames checks for the same task name appearing in multiple states.
// When duplicates are found, prompts the user to choose which copy to keep.
func (r *Runner) fixDuplicateTaskNames() (int, error) {
	seen := make(map[string][]design.Task)

	for _, state := range []design.TaskState{
		design.StatePending, design.StateReview, design.StateMerge,
		design.StateCompleted, design.StateAbandoned,
	} {
		tasks, err := r.Design.TasksByState(state)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			seen[t.Name] = append(seen[t.Name], t)
		}
	}

	reader := bufio.NewReader(os.Stdin)
	issues := 0
	for name, tasks := range seen {
		if len(tasks) <= 1 {
			continue
		}
		issues++

		fmt.Printf("CONFLICT: task %q exists in %d states:\n", name, len(tasks))
		for i, t := range tasks {
			label := string(t.State)
			if t.Group != "" {
				label += " (group: " + t.Group + ")"
			}
			fmt.Printf("  [%d] %s — %s\n", i+1, label, t.FilePath)
		}
		fmt.Printf("  [s] skip (do nothing)\n")
		fmt.Printf("Which copy to keep? ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "s" || input == "" {
			fmt.Printf("  Skipped.\n")
			continue
		}

		choice, err := strconv.Atoi(input)
		if err != nil || choice < 1 || choice > len(tasks) {
			fmt.Printf("  Invalid choice, skipping.\n")
			continue
		}

		// Delete all copies except the chosen one.
		for i, t := range tasks {
			if i == choice-1 {
				continue
			}
			if err := r.Design.DeleteTask(&t); err != nil {
				fmt.Printf("  ERROR: could not remove %s: %v\n", t.FilePath, err)
			} else {
				fmt.Printf("  FIXED: removed %s copy (%s)\n", t.State, t.FilePath)
			}
		}
	}

	return issues, nil
}

// fixStaleLocks removes lock files held by dead processes.
func (r *Runner) fixStaleLocks(baseDir string) (int, error) {
	hydraDir := config.HydraPath(baseDir)
	pattern := filepath.Join(hydraDir, "hydra-*.lock")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, err
	}

	issues := 0
	for _, path := range matches {
		data, err := os.ReadFile(path) //nolint:gosec // lock files in hydra dir
		if err != nil {
			continue
		}

		// Check if the lock's process is still alive using lock.ReadAll logic.
		// We parse manually since lock internals aren't exported.
		_ = data // parsed below via ReadAll
	}

	// Use ReadAll to get live locks, then compare against all lock files.
	live, _ := lock.ReadAll(hydraDir)
	liveSet := make(map[string]bool)
	for _, rt := range live {
		liveSet[rt.TaskName] = true
	}

	for _, path := range matches {
		base := filepath.Base(path)
		// Check if this lock file corresponds to any live task.
		isLive := false
		for _, rt := range live {
			expected := "hydra-" + sanitizeLockName(rt.TaskName) + ".lock"
			if base == expected {
				isLive = true
				break
			}
		}
		if !isLive {
			issues++
			fmt.Printf("FIXED: removed stale lock %s\n", base)
			_ = os.Remove(path)
		}
	}

	return issues, nil
}

// sanitizeLockName matches the lock package's slash-to-dash conversion.
func sanitizeLockName(name string) string {
	result := make([]byte, len(name))
	for i := range len(name) {
		if name[i] == '/' {
			result[i] = '-'
			// The lock package uses "--" for slashes.
			// We need to match lockFileName exactly.
		} else {
			result[i] = name[i]
		}
	}
	// Actually re-implement the exact logic from the lock package.
	out := ""
	for _, c := range name {
		if c == '/' {
			out += "--"
		} else {
			out += string(c)
		}
	}
	return out
}

// fixWorkDirBranches checks that work directories are on the correct branch.
func (r *Runner) fixWorkDirBranches(baseDir string) (int, error) {
	tasks, err := r.Design.AllTasks()
	if err != nil {
		return 0, err
	}

	issues := 0
	for _, task := range tasks {
		wd := r.workDir(&task)
		if !repo.IsGitRepo(wd) {
			continue
		}

		taskRepo := repo.Open(wd)
		currentBranch, err := taskRepo.CurrentBranch()
		if err != nil {
			continue
		}

		expectedBranch := task.BranchName()

		// Tasks in completed/abandoned state don't need branch checks.
		if task.State == design.StateCompleted || task.State == design.StateAbandoned {
			continue
		}

		if currentBranch != expectedBranch {
			issues++
			if taskRepo.BranchExists(expectedBranch) {
				if err := taskRepo.Checkout(expectedBranch); err == nil {
					fmt.Printf("FIXED: %s checked out to %s (was %s)\n", task.Name, expectedBranch, currentBranch)
				} else {
					fmt.Printf("ERROR: %s on %s, expected %s (checkout failed: %v)\n", task.Name, currentBranch, expectedBranch, err)
				}
			} else {
				fmt.Printf("WARN: %s on %s, expected %s (branch does not exist)\n", task.Name, currentBranch, expectedBranch)
			}
		}
	}

	return issues, nil
}

// fixWorkDirRemotes checks that work directory remotes point to the configured source repo.
func (r *Runner) fixWorkDirRemotes(baseDir string) (int, error) {
	tasks, err := r.Design.AllTasks()
	if err != nil {
		return 0, err
	}

	expected := r.Config.SourceRepoURL
	issues := 0

	for _, task := range tasks {
		wd := r.workDir(&task)
		if !repo.IsGitRepo(wd) {
			continue
		}

		taskRepo := repo.Open(wd)
		remote, err := taskRepo.RemoteURL()
		if err != nil {
			issues++
			fmt.Printf("ERROR: %s has no origin remote\n", task.Name)
			continue
		}

		if remote != expected {
			issues++
			fmt.Printf("MISMATCH: %s remote is %s, expected %s\n", task.Name, remote, expected)
		}
	}

	// Also check the special work dirs.
	for _, name := range []string{"_reconcile", "_verify"} {
		wd := filepath.Join(baseDir, "work", name)
		if !repo.IsGitRepo(wd) {
			continue
		}
		taskRepo := repo.Open(wd)
		remote, err := taskRepo.RemoteURL()
		if err != nil {
			continue
		}
		if remote != expected {
			issues++
			fmt.Printf("MISMATCH: %s remote is %s, expected %s\n", name, remote, expected)
		}
	}

	return issues, nil
}

// fixMissingStateDirs ensures all state directories exist.
func (r *Runner) fixMissingStateDirs() int {
	dirs := []string{
		filepath.Join(r.Design.Path, "tasks"),
		filepath.Join(r.Design.Path, "state", "review"),
		filepath.Join(r.Design.Path, "state", "merge"),
		filepath.Join(r.Design.Path, "state", "completed"),
		filepath.Join(r.Design.Path, "state", "abandoned"),
	}

	issues := 0
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			issues++
			if err := os.MkdirAll(dir, 0o750); err == nil {
				fmt.Printf("FIXED: created missing directory %s\n", dir)
			} else {
				fmt.Printf("ERROR: could not create %s: %v\n", dir, err)
			}
		}
	}

	return issues
}

// fixOrphanedWorkDirs finds work directories that have no corresponding task.
func (r *Runner) fixOrphanedWorkDirs(baseDir string) (int, error) {
	workRoot := filepath.Join(baseDir, "work")
	if _, err := os.Stat(workRoot); os.IsNotExist(err) {
		return 0, nil
	}

	tasks, err := r.Design.AllTasks()
	if err != nil {
		return 0, err
	}

	// Build set of expected work dir paths.
	expected := make(map[string]bool)
	for _, t := range tasks {
		expected[r.workDir(&t)] = true
	}
	// Add special dirs.
	expected[filepath.Join(baseDir, "work", "_reconcile")] = true
	expected[filepath.Join(baseDir, "work", "_verify")] = true

	issues := 0

	entries, err := os.ReadDir(workRoot)
	if err != nil {
		return 0, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(workRoot, entry.Name())

		// Check if it's a direct work dir.
		if expected[entryPath] {
			continue
		}

		// Could be a group directory — check children.
		if isGroupWorkDir(entryPath) {
			subEntries, err := os.ReadDir(entryPath)
			if err != nil {
				continue
			}
			for _, sub := range subEntries {
				if !sub.IsDir() {
					continue
				}
				subPath := filepath.Join(entryPath, sub.Name())
				if !expected[subPath] {
					issues++
					fmt.Printf("ORPHAN: work directory %s has no corresponding task\n", subPath)
				}
			}
			continue
		}

		// Not expected and not a group dir.
		issues++
		fmt.Printf("ORPHAN: work directory %s has no corresponding task\n", entryPath)
	}

	return issues, nil
}

// isGroupWorkDir returns true if the directory contains subdirectories
// (suggesting it's a group work dir like work/backend/).
func isGroupWorkDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
	}
	return false
}
