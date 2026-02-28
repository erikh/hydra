package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/erikh/hydra/internal/repo"
)

// Verify uses Claude to verify that all items in functional.md are satisfied
// by the current codebase.
func (r *Runner) Verify() error {
	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}

	// Read functional.md.
	functional, err := r.Design.Functional()
	if err != nil {
		return fmt.Errorf("reading functional.md: %w", err)
	}
	if strings.TrimSpace(functional) == "" {
		return errors.New("functional.md is empty; nothing to verify")
	}

	// Prepare work directory.
	wd := filepath.Join(baseDir, "work", "_verify")
	verifyRepo, err := r.prepareRepo(wd)
	if err != nil {
		return fmt.Errorf("preparing work directory: %w", err)
	}

	// Fetch and rebase against origin/main to ensure we verify the latest code.
	// Skip rebase if the working tree is dirty — let Claude work on it as-is.
	if err := verifyRepo.Fetch(); err != nil {
		return fmt.Errorf("fetching origin: %w", err)
	}
	if dirty, _ := verifyRepo.HasChanges(); !dirty {
		defaultBranch, err := r.detectDefaultBranch(verifyRepo)
		if err != nil {
			return fmt.Errorf("detecting default branch: %w", err)
		}
		if err := verifyRepo.Rebase("origin/" + defaultBranch); err != nil {
			return fmt.Errorf("rebasing against origin/%s: %w", defaultBranch, err)
		}
	}

	// Run before hook.
	if err := r.runBeforeHook(wd); err != nil {
		return fmt.Errorf("before hook: %w", err)
	}

	// Assemble document.
	sign := verifyRepo.HasSigningKey()
	cmds := r.commandsMap(wd)
	doc, err := r.assembleVerifyDocument(functional, sign, cmds)
	if err != nil {
		return fmt.Errorf("assembling verify document: %w", err)
	}

	// Capture HEAD before invoking Claude.
	beforeSHA, _ := verifyRepo.LastCommitSHA()

	// Invoke Claude.
	claudeFn := r.Claude
	if claudeFn == nil {
		claudeFn = invokeClaude
	}
	err = claudeFn(context.Background(), ClaudeRunConfig{
		RepoDir:    wd,
		Document:   doc,
		Model:      r.Model,
		AutoAccept: r.AutoAccept,
		PlanMode:   r.PlanMode,
	})
	if err != nil {
		return fmt.Errorf("claude failed: %w", err)
	}

	// Check for verify-passed.txt or verify-failed.txt.
	passedPath := filepath.Join(wd, "verify-passed.txt")
	failedPath := filepath.Join(wd, "verify-failed.txt")

	if _, err := os.Stat(passedPath); err == nil {
		fmt.Println("All functional requirements verified.")

		if err := r.pushVerifyFixes(verifyRepo, beforeSHA); err != nil {
			return err
		}

		if syncErr := r.Sync(nil); syncErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: post-verify sync failed: %v\n", syncErr)
		}
		return nil
	}

	if _, err := os.Stat(failedPath); err == nil {
		data, readErr := os.ReadFile(failedPath) //nolint:gosec // path is constructed from our own work dir
		if readErr != nil {
			return fmt.Errorf("reading verify-failed.txt: %w", readErr)
		}
		fmt.Println("Verification failed:")
		fmt.Println(string(data))
		return errors.New("functional requirements verification failed")
	}

	return errors.New("claude did not produce verify-passed.txt or verify-failed.txt")
}

// assembleVerifyDocument builds the prompt for the verify workflow.
func (r *Runner) assembleVerifyDocument(functional string, sign bool, cmds map[string]string) (string, error) {
	rules, err := r.Design.Rules()
	if err != nil {
		return "", err
	}

	lint, err := r.Design.Lint()
	if err != nil {
		return "", err
	}

	var b strings.Builder

	b.WriteString("# Mission\n\nYour objective is to verify that every requirement in the functional specification " +
		"below is satisfied by the current codebase. If code does not match the specification, fix the code.\n\n")

	if rules != "" {
		b.WriteString("# Rules\n\n")
		b.WriteString(rules)
		b.WriteString("\n\n")
	}
	if lint != "" {
		b.WriteString("# Lint Rules\n\n")
		b.WriteString(lint)
		b.WriteString("\n\n")
	}

	b.WriteString("# Functional Specification\n\n")
	b.WriteString(functional)
	b.WriteString("\n\n")

	b.WriteString("# Verification Instructions\n\n")
	b.WriteString("For each requirement in the specification above:\n")
	b.WriteString("1. Find the relevant code that implements it\n")
	b.WriteString("2. Confirm the implementation matches the specification\n")
	b.WriteString("3. If the code does not satisfy a requirement, fix the code to match the specification\n")
	b.WriteString("4. Verify that the requirement has adequate test coverage — there should be tests that exercise the described behavior, including edge cases and error paths\n")
	b.WriteString("5. Run tests according to the hydra.yml test task, serially\n\n")

	b.WriteString(verificationSection(cmds))

	b.WriteString("\nIf ALL requirements are satisfied, all have adequate test coverage, and all tests pass, " +
		"create a file called `verify-passed.txt` containing \"PASS\" and nothing else.\n\n")

	b.WriteString("If ANY requirement is NOT satisfied or lacks adequate test coverage, " +
		"create a file called `verify-failed.txt` listing each failed requirement and why it failed " +
		"(including any that lack tests).\n\n")

	b.WriteString("Do not modify the functional specification. " +
		"The specification is the source of truth — if code does not match the specification, fix the code.\n")

	b.WriteString(commitInstructions(sign, cmds))

	b.WriteString("\n# Reminder\n\n")
	b.WriteString("The functional specification is authoritative. Fix code to match it, never the reverse. " +
		"Commit your changes, then create verify-passed.txt or verify-failed.txt when done.\n")

	return b.String(), nil
}

// pushVerifyFixes rebases and pushes if Claude committed changes during verify.
func (r *Runner) pushVerifyFixes(verifyRepo *repo.Repo, beforeSHA string) error {
	afterSHA, _ := verifyRepo.LastCommitSHA()
	if afterSHA == beforeSHA {
		return nil
	}

	if err := verifyRepo.Fetch(); err != nil {
		return fmt.Errorf("fetching origin before push: %w", err)
	}
	defaultBranch, err := r.detectDefaultBranch(verifyRepo)
	if err != nil {
		return fmt.Errorf("detecting default branch: %w", err)
	}
	if err := verifyRepo.Rebase("origin/" + defaultBranch); err != nil {
		return fmt.Errorf("rebasing against origin/%s before push: %w", defaultBranch, err)
	}
	if err := verifyRepo.PushMain(); err != nil {
		return fmt.Errorf("pushing: %w", err)
	}
	fmt.Println("Pushed verify fixes to origin.")
	return nil
}
