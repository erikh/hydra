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

// fixAction describes a single issue found by the scanner and a function to fix it.
type fixAction struct {
	description string
	fix         func() error
}

// Fix scans the project for issues, reports them, and prompts for confirmation
// before applying fixes. Duplicate task conflicts are handled interactively
// before the main scan. If autoConfirm is true, fixes are applied without prompting.
// Returns an error only if scanning itself fails, not for individual issues.
func (r *Runner) Fix(autoConfirm bool) error {
	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}

	// Handle duplicate task conflicts first (interactive — requires per-conflict choices).
	dupes, err := r.fixDuplicateTaskNames()
	if err != nil {
		return fmt.Errorf("checking duplicate tasks: %w", err)
	}

	// Scan for all other fixable issues.
	var actions []fixAction

	a, err := r.scanStaleLocks(baseDir)
	if err != nil {
		return fmt.Errorf("checking stale locks: %w", err)
	}
	actions = append(actions, a...)

	a, err = r.scanWorkDirBranches(baseDir)
	if err != nil {
		return fmt.Errorf("checking work directories: %w", err)
	}
	actions = append(actions, a...)

	a = r.scanMissingStateDirs()
	actions = append(actions, a...)

	a, err = r.scanOrphanedWorkDirs(baseDir)
	if err != nil {
		return fmt.Errorf("checking orphaned work dirs: %w", err)
	}
	actions = append(actions, a...)

	a, err = r.scanStuckMergeTasks()
	if err != nil {
		return fmt.Errorf("checking stuck merge tasks: %w", err)
	}
	actions = append(actions, a...)

	// Report non-fixable issues (remotes).
	warns, err := r.scanWorkDirRemotes(baseDir)
	if err != nil {
		return fmt.Errorf("checking remotes: %w", err)
	}
	for _, w := range warns {
		fmt.Println(w)
	}

	total := dupes + len(actions) + len(warns)
	if total == 0 {
		fmt.Println("No issues found.")
		return nil
	}

	if len(actions) == 0 {
		fmt.Printf("\n%d issue(s) found.\n", total)
		return nil
	}

	// Report what will be fixed.
	fmt.Printf("\nIssues to fix:\n")
	for i, a := range actions {
		fmt.Printf("  %d. %s\n", i+1, a.description)
	}

	// Prompt for confirmation unless auto-confirmed.
	if !autoConfirm {
		fmt.Printf("\nApply %d fix(es)? [y/N] ", len(actions))
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read input: %v\n", err)
			fmt.Println("Aborted.")
			return nil
		}
		input = strings.TrimSpace(strings.ToLower(input))

		if input != "y" && input != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Apply fixes.
	for _, a := range actions {
		if err := a.fix(); err != nil {
			fmt.Printf("ERROR: %s: %v\n", a.description, err)
		} else {
			fmt.Printf("FIXED: %s\n", a.description)
		}
	}

	fmt.Printf("\n%d issue(s) found, %d fix(es) applied.\n", total, len(actions))
	return nil
}

// fixDuplicateTaskNames checks for the same task name appearing in multiple states.
// When duplicates are found, prompts the user to choose which copy to keep.
// Returns the number of conflicts found.
func (r *Runner) fixDuplicateTaskNames() (int, error) { //nolint:unparam // error kept for future use
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

		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read input: %v\n", err)
			fmt.Printf("  Skipped.\n")
			continue
		}
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

// scanStaleLocks finds lock files held by dead processes.
func (r *Runner) scanStaleLocks(baseDir string) ([]fixAction, error) {
	hydraDir := config.HydraPath(baseDir)
	pattern := filepath.Join(hydraDir, "hydra-*.lock")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Use ReadAll to get live locks, then compare against all lock files.
	live, err := lock.ReadAll(hydraDir)
	if err != nil {
		return nil, fmt.Errorf("reading live locks: %w", err)
	}

	var actions []fixAction
	for _, path := range matches {
		base := filepath.Base(path)
		isLive := false
		for _, rt := range live {
			expected := "hydra-" + sanitizeLockName(rt.TaskName) + ".lock"
			if base == expected {
				isLive = true
				break
			}
		}
		if !isLive {
			p := path // capture for closure
			actions = append(actions, fixAction{
				description: "remove stale lock " + base,
				fix:         func() error { return os.Remove(p) },
			})
		}
	}

	return actions, nil
}

// sanitizeLockName matches the lock package's slash-to-dash conversion.
func sanitizeLockName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if c == '/' {
			b.WriteString("--")
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// scanWorkDirBranches checks that work directories are on the correct branch.
func (r *Runner) scanWorkDirBranches(_ string) ([]fixAction, error) {
	tasks, err := r.Design.AllTasks()
	if err != nil {
		return nil, err
	}

	var actions []fixAction
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
			tr := taskRepo // capture
			eb := expectedBranch
			tn := task.Name
			cb := currentBranch
			if taskRepo.BranchExists(expectedBranch) {
				actions = append(actions, fixAction{
					description: fmt.Sprintf("checkout %s to %s (currently %s)", tn, eb, cb),
					fix:         func() error { return tr.Checkout(eb) },
				})
			} else {
				// Can't fix this one, just warn.
				fmt.Printf("WARN: %s on %s, expected %s (branch does not exist)\n", tn, cb, eb)
			}
		}
	}

	return actions, nil
}

// scanWorkDirRemotes checks that work directory remotes point to the configured source repo.
// Returns warning strings since remote mismatches can't be auto-fixed.
func (r *Runner) scanWorkDirRemotes(baseDir string) ([]string, error) {
	tasks, err := r.Design.AllTasks()
	if err != nil {
		return nil, err
	}

	expected := r.Config.SourceRepoURL
	var warns []string

	for _, task := range tasks {
		wd := r.workDir(&task)
		if !repo.IsGitRepo(wd) {
			continue
		}

		taskRepo := repo.Open(wd)
		remote, err := taskRepo.RemoteURL()
		if err != nil {
			warns = append(warns, fmt.Sprintf("ERROR: %s has no origin remote", task.Name))
			continue
		}

		if remote != expected {
			warns = append(warns, fmt.Sprintf("MISMATCH: %s remote is %s, expected %s", task.Name, remote, expected))
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
			warns = append(warns, fmt.Sprintf("MISMATCH: %s remote is %s, expected %s", name, remote, expected))
		}
	}

	return warns, nil
}

// scanMissingStateDirs finds state directories that don't exist.
func (r *Runner) scanMissingStateDirs() []fixAction {
	dirs := []string{
		filepath.Join(r.Design.Path, "tasks"),
		filepath.Join(r.Design.Path, "state", "review"),
		filepath.Join(r.Design.Path, "state", "merge"),
		filepath.Join(r.Design.Path, "state", "completed"),
		filepath.Join(r.Design.Path, "state", "abandoned"),
	}

	var actions []fixAction
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			d := dir // capture
			actions = append(actions, fixAction{
				description: "create missing directory " + d,
				fix:         func() error { return os.MkdirAll(d, 0o750) },
			})
		}
	}

	return actions
}

// scanOrphanedWorkDirs finds work directories that have no corresponding task.
func (r *Runner) scanOrphanedWorkDirs(baseDir string) ([]fixAction, error) {
	workRoot := filepath.Join(baseDir, "work")
	if _, err := os.Stat(workRoot); os.IsNotExist(err) {
		return nil, nil
	}

	tasks, err := r.Design.AllTasks()
	if err != nil {
		return nil, err
	}

	// Build sets: leaf dirs are task work dirs (don't recurse into),
	// parent dirs are group directories (recurse into to check children).
	leafDirs := make(map[string]bool)
	parentDirs := make(map[string]bool)
	for _, t := range tasks {
		wd := r.workDir(&t)
		leafDirs[wd] = true
		if t.Group != "" {
			parentDirs[filepath.Dir(wd)] = true
		}
	}
	// Special dirs are also leaves.
	leafDirs[filepath.Join(baseDir, "work", "_reconcile")] = true
	leafDirs[filepath.Join(baseDir, "work", "_verify")] = true

	return r.collectOrphanedWorkDirs(workRoot, leafDirs, parentDirs)
}

// collectOrphanedWorkDirs recursively walks a work directory tree, collecting
// fixActions for directories not in the expected sets.
func (r *Runner) collectOrphanedWorkDirs(dir string, leafDirs, parentDirs map[string]bool) ([]fixAction, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var actions []fixAction
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(dir, entry.Name())

		if leafDirs[entryPath] {
			// Known task work dir — skip (don't recurse into it).
			continue
		}

		if parentDirs[entryPath] {
			// Group parent dir — recurse to check children.
			a, err := r.collectOrphanedWorkDirs(entryPath, leafDirs, parentDirs)
			if err != nil {
				return actions, err
			}
			actions = append(actions, a...)
			continue
		}

		// Not expected — schedule teardown and removal.
		p := entryPath // capture
		actions = append(actions, fixAction{
			description: "remove orphaned work directory " + p,
			fix: func() error {
				r.runTeardown(p)
				return os.RemoveAll(p)
			},
		})
	}

	return actions, nil
}

// scanStuckMergeTasks finds tasks stuck in merge state with no active lock.
func (r *Runner) scanStuckMergeTasks() ([]fixAction, error) {
	tasks, err := r.Design.TasksByState(design.StateMerge)
	if err != nil {
		return nil, err
	}

	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}
	hydraDir := config.HydraPath(baseDir)

	var actions []fixAction
	for i := range tasks {
		t := &tasks[i]
		taskName := t.Name
		if t.Group != "" {
			taskName = t.Group + "/" + t.Name
		}

		// Check if there's an active lock for this merge.
		lk := lock.New(hydraDir, "merge:"+taskName)
		if lk.IsHeld() {
			continue
		}

		// No lock — this task is stuck in merge state.
		task := t // capture
		actions = append(actions, fixAction{
			description: fmt.Sprintf("move stuck task %q from merge back to review", taskName),
			fix:         func() error { return r.Design.MoveTask(task, design.StateReview) },
		})
	}

	return actions, nil
}
