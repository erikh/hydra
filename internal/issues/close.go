package issues

import (
	"strconv"
	"strings"

	"github.com/erikh/hydra/internal/design"
)

// Closer is the interface for closing issues on a remote tracker.
type Closer interface {
	CloseIssue(number int, comment string) error
}

// ParseIssueTaskNumber extracts the issue number from a task name like "42-fix-bug".
// Returns 0 if the name doesn't start with a number.
func ParseIssueTaskNumber(taskName string) int {
	parts := strings.SplitN(taskName, "-", 2)
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// IsIssueTask returns true if the task belongs to the "issues" group.
func IsIssueTask(task *design.Task) bool {
	return task.Group == "issues"
}
