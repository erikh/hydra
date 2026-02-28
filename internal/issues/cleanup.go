package issues

import (
	"fmt"
	"os"
	"strings"

	"github.com/erikh/hydra/internal/design"
	"github.com/erikh/hydra/internal/repo"
)

// CleanupResult holds the counts from a Cleanup run.
type CleanupResult struct {
	BranchesDeleted int
	IssuesClosed    int
}

// Cleanup iterates over completed and abandoned tasks, deletes their remote
// branches, and closes associated issues with a thank-you comment that
// includes the commit SHA from the record.
func Cleanup(dd *design.Dir, sourceRepo *repo.Repo, closer Closer) (*CleanupResult, error) {
	result := &CleanupResult{}

	// Load the record to find commit SHAs for tasks.
	record := design.NewRecord(dd.Path)
	entries, err := record.Entries()
	if err != nil {
		return nil, fmt.Errorf("reading record: %w", err)
	}

	// Build a map from task name to merge SHA.
	// Record entries use "merge:taskName" for merge commits.
	mergeSHAs := make(map[string]string)
	for _, e := range entries {
		name := e.TaskName
		if after, ok := strings.CutPrefix(name, "merge:"); ok {
			name = after
		}
		mergeSHAs[name] = e.SHA
	}

	for _, state := range []design.TaskState{design.StateCompleted, design.StateAbandoned} {
		tasks, err := dd.TasksByState(state)
		if err != nil {
			return nil, fmt.Errorf("listing %s tasks: %w", state, err)
		}

		for _, task := range tasks {
			branch := task.BranchName()

			// Delete remote branch (ignore errors — branch may already be gone).
			if err := sourceRepo.DeleteRemoteBranch(branch); err == nil {
				result.BranchesDeleted++
			}

			// Close associated issue if this is an issue task.
			if closer == nil || !IsIssueTask(&task) {
				continue
			}
			num := ParseIssueTaskNumber(task.Name)
			if num == 0 {
				continue
			}

			taskRef := task.Name
			if task.Group != "" {
				taskRef = task.Group + "/" + task.Name
			}
			sha := mergeSHAs[taskRef]

			comment := fmt.Sprintf(
				"Thanks for reporting this! It has been addressed by hydra in commit %s.",
				sha,
			)
			if sha == "" {
				comment = "Thanks for reporting this! It has been addressed by hydra."
			}

			if err := closer.CloseIssue(num, comment); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not close issue #%d: %v\n", num, err)
			} else {
				result.IssuesClosed++
			}
		}
	}

	return result, nil
}
