package runner

import (
	"fmt"
	"os/exec"
	"strings"
)

func invokeClaude(repoDir, document string) error {
	cmd := exec.Command("claude", "-p", "--dangerously-skip-permissions")
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(document)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude invocation failed: %s\n%s", err, out)
	}

	return nil
}
