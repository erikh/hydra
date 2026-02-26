package runner

import "strings"

// commitInstructions returns a markdown section instructing Claude to
// run tests/lint, stage changes, and commit with a descriptive message.
func commitInstructions(sign bool, commands map[string]string) string {
	var b strings.Builder
	b.WriteString("\n\n# Commit Instructions\n\n")
	b.WriteString("After making all code changes, follow these steps:\n\n")

	if testCmd, ok := commands["test"]; ok && testCmd != "" {
		b.WriteString("1. Run the test suite: `")
		b.WriteString(testCmd)
		b.WriteString("`\n")
	}
	if lintCmd, ok := commands["lint"]; ok && lintCmd != "" {
		b.WriteString("2. Run the linter: `")
		b.WriteString(lintCmd)
		b.WriteString("`\n")
	}

	b.WriteString("3. Stage all changes: `git add -A`\n")
	b.WriteString("4. Commit with a descriptive message. ")

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
