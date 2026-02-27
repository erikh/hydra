package runner

import (
	"fmt"
	"strings"
	"time"
)

// verificationSection returns a markdown section listing the test and lint
// commands Claude should run before committing. Returns empty string if
// no commands are configured.
func verificationSection(commands map[string]string) string {
	testCmd := commands["test"]
	lintCmd := commands["lint"]

	if testCmd == "" && lintCmd == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n## Verification\n\n")
	b.WriteString("Before committing, ensure all checks pass. " +
		"The commands below are the project's official test and lint commands from hydra.yml. " +
		"Do not run other commands to perform testing or linting. " +
		"Only run the exact commands listed below, fix any issues they report, and repeat until they pass.\n\n")

	if testCmd != "" {
		b.WriteString("- Run tests: `")
		b.WriteString(testCmd)
		b.WriteString("`\n")
	}
	if lintCmd != "" {
		b.WriteString("- Run linter: `")
		b.WriteString(lintCmd)
		b.WriteString("`\n")
	}

	b.WriteString("\nIMPORTANT: Multiple hydra tasks may run concurrently, each in its own " +
		"work directory. Do not modify these commands to use fixed ports, shared temp files, " +
		"or any global state that would conflict with parallel runs. " +
		"All test and lint operations must be fully isolated to the current working tree.\n")
	return b.String()
}

// commitInstructions returns a markdown section instructing Claude to
// run tests/lint, stage changes, and commit with a descriptive message.
func commitInstructions(sign bool, commands map[string]string) string {
	var b strings.Builder
	b.WriteString("\n\n# Commit Instructions\n\n")

	b.WriteString("IMPORTANT: Do NOT run any individual test files, test functions, " +
		"lint checks, or any other testing/linting tools manually. " +
		"The ONLY test and lint commands you may run are the exact commands listed below " +
		"from hydra.yml. Do not invoke test runners, linters, or type checkers in any other way.\n\n")

	b.WriteString("After making all code changes, follow the steps below.\n\n")

	step := 1
	if testCmd, ok := commands["test"]; ok && testCmd != "" {
		b.WriteString(stepPrefix(step))
		b.WriteString("Run the test suite: `")
		b.WriteString(testCmd)
		b.WriteString("`\n")
		step++
	}
	if lintCmd, ok := commands["lint"]; ok && lintCmd != "" {
		b.WriteString(stepPrefix(step))
		b.WriteString("Run the linter: `")
		b.WriteString(lintCmd)
		b.WriteString("`\n")
		step++
	}

	b.WriteString(stepPrefix(step))
	b.WriteString("Stage all changes: `git add -A`\n")
	step++
	b.WriteString(stepPrefix(step))
	b.WriteString("Commit with a descriptive message. ")

	if sign {
		b.WriteString("Sign the commit: `git commit -S -m \"<descriptive message>\"`\n")
	} else {
		b.WriteString("Commit: `git commit -m \"<descriptive message>\"`\n")
	}

	b.WriteString("\nIMPORTANT: You MUST commit your changes before finishing. ")
	b.WriteString("The commit message should describe what was done, not just the task name. ")
	b.WriteString("Do NOT add Co-Authored-By or any other trailers to the commit message.\n")

	return b.String()
}

// notificationSection returns a markdown section instructing Claude to send
// a desktop notification whenever it needs user confirmation or attention.
func notificationSection() string {
	return "\n\n# Desktop Notifications\n\n" +
		"Whenever you need user confirmation, approval, or attention — such as presenting " +
		"a plan for review, encountering an error you cannot resolve, or reaching a decision " +
		"point — send a desktop notification with a brief description of what needs attention.\n"
}

// missionReminder returns a closing section that reinforces task focus.
func missionReminder() string {
	return "\n\n# Reminder\n\n" +
		"Your ONLY job is the task described in the document above. " +
		"Do not make unrelated changes, refactor other code, or work on anything " +
		"outside the scope of the task. Stay focused on the mission.\n"
}

// timeoutSection returns a markdown section instructing Claude to complete
// within the given timeout. Returns empty string if timeout is zero.
func timeoutSection(timeout time.Duration) string {
	if timeout <= 0 {
		return ""
	}
	return fmt.Sprintf("\n\n# Time Limit\n\n"+
		"You have %s to complete this task. "+
		"If you are running low on time, commit whatever progress you have made so far "+
		"and stop. A partial commit that builds and passes tests is better than no commit at all.\n", timeout)
}

// stepPrefix returns a numbered step prefix like "1. ", "2. ", etc.
func stepPrefix(n int) string {
	return fmt.Sprintf("%d. ", n)
}
