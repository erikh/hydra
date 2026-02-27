// Package notify provides desktop notification support.
package notify

import (
	"os/exec"
	"fmt"
)

// Send sends a desktop notification via macOS Notification Center.
func Send(title, message string) error {
	script := fmt.Sprintf(`display notification %q with title %q`, message, title)
	return exec.Command("osascript", "-e", script).Run()
}
