package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/erikh/hydra/internal/design"
)

// Reconcile reads all completed tasks, uses Claude to merge their requirements
// into functional.md, then deletes the completed task files.
func (r *Runner) Reconcile() error {
	baseDir := r.BaseDir
	if baseDir == "" {
		baseDir = "."
	}

	// Get all completed tasks.
	completed, err := r.Design.TasksByState(design.StateCompleted)
	if err != nil {
		return fmt.Errorf("listing completed tasks: %w", err)
	}
	if len(completed) == 0 {
		return errors.New("no completed tasks to reconcile")
	}

	// Read current functional.md.
	functional, err := r.Design.Functional()
	if err != nil {
		return fmt.Errorf("reading functional.md: %w", err)
	}

	// Read all completed task contents.
	var taskContents []taskEntry
	for _, task := range completed {
		content, err := task.Content()
		if err != nil {
			return fmt.Errorf("reading task %s: %w", task.Name, err)
		}
		name := task.Name
		if task.Group != "" {
			name = task.Group + "/" + task.Name
		}
		taskContents = append(taskContents, taskEntry{name: name, content: content})
	}

	// Prepare work directory.
	wd := filepath.Join(baseDir, "work", "_reconcile")
	reconcileRepo, err := r.prepareRepo(wd)
	if err != nil {
		return fmt.Errorf("preparing work directory: %w", err)
	}

	// Fetch and rebase against origin/main to ensure we reconcile against the latest code.
	// Skip rebase if the working tree is dirty — let Claude work on it as-is.
	if err := reconcileRepo.Fetch(); err != nil {
		return fmt.Errorf("fetching origin: %w", err)
	}
	dirty, err := reconcileRepo.HasChanges()
	if err != nil {
		return fmt.Errorf("checking working tree: %w", err)
	}
	if !dirty {
		defaultBranch, err := r.detectDefaultBranch(reconcileRepo)
		if err != nil {
			return fmt.Errorf("detecting default branch: %w", err)
		}
		if err := reconcileRepo.Rebase("origin/" + defaultBranch); err != nil {
			return fmt.Errorf("rebasing against origin/%s: %w", defaultBranch, err)
		}
	}

	// Copy current functional.md into the work directory for Claude to edit.
	functionalPath := filepath.Join(wd, "functional.md")
	if err := os.WriteFile(functionalPath, []byte(functional), 0o600); err != nil {
		return fmt.Errorf("writing functional.md to work dir: %w", err)
	}

	// Assemble the document.
	doc := assembleReconcileDocument(functional, taskContents)

	// Run before hook.
	if err := r.runBeforeHook(wd); err != nil {
		return fmt.Errorf("before hook: %w", err)
	}

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

	// Read updated functional.md from work dir.
	updatedData, err := os.ReadFile(functionalPath) //nolint:gosec // path is constructed from our own work dir
	if err != nil {
		return fmt.Errorf("reading updated functional.md: %w", err)
	}
	updated := string(updatedData)

	// Copy back to design dir if changed.
	if updated != functional {
		designFunctionalPath := filepath.Join(r.Design.Path, "functional.md")
		if err := os.WriteFile(designFunctionalPath, []byte(updated), 0o600); err != nil {
			return fmt.Errorf("writing functional.md to design dir: %w", err)
		}
		fmt.Println("Updated functional.md with reconciled requirements.")
	} else {
		fmt.Println("functional.md unchanged.")
	}

	// Delete completed task files.
	for i := range completed {
		if err := r.Design.DeleteTask(&completed[i]); err != nil {
			return fmt.Errorf("deleting completed task %s: %w", completed[i].Name, err)
		}
	}

	fmt.Printf("Deleted %d completed task(s).\n", len(completed))
	return nil
}

// taskEntry holds a task name and its content for document assembly.
type taskEntry struct {
	name    string
	content string
}

// assembleReconcileDocument builds the prompt for the reconcile workflow.
func assembleReconcileDocument(functional string, tasks []taskEntry) string {
	var b strings.Builder

	b.WriteString("# Mission\n\nYour sole objective is to update the functional specification " +
		"based on the completed tasks listed below. Do not make any other changes. " +
		"Do not modify any source code files. Only edit functional.md.\n\n")

	b.WriteString("# Current Functional Specification\n\n")
	if functional != "" {
		b.WriteString(functional)
	} else {
		b.WriteString("No existing specification.")
	}
	b.WriteString("\n\n")

	b.WriteString("# Completed Tasks\n\n")
	for _, t := range tasks {
		b.WriteString("## ")
		b.WriteString(t.name)
		b.WriteString("\n\n")
		b.WriteString(t.content)
		b.WriteString("\n\n")
	}

	b.WriteString("# Instructions\n\n")
	b.WriteString("Read the codebase to understand what was actually implemented for each completed task. ")
	b.WriteString("Then update the file `functional.md` in the current directory. This file is the project's ")
	b.WriteString("living functional specification. Merge the requirements from the completed tasks above ")
	b.WriteString("into it, removing duplicates and organizing by feature area. The result should be a ")
	b.WriteString("concise, accurate description of what the software does — not a list of tasks, but a ")
	b.WriteString("specification of behaviors and capabilities. Use the actual code as ground truth.\n\n")

	b.WriteString("Do not make any other changes. Do not modify any source code files. Only edit functional.md.\n")

	b.WriteString("\n# Reminder\n\n")
	b.WriteString("Your ONLY job is to update functional.md. Do not make any other changes.\n")

	return b.String()
}
