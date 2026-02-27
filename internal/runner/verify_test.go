package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyPass(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Mock Claude to create verify-passed.txt.
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		return os.WriteFile(filepath.Join(cfg.RepoDir, "verify-passed.txt"), []byte("PASS"), 0o600)
	}

	if err := r.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyFail(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Mock Claude to create verify-failed.txt.
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		content := "- Requirement X: not implemented\n- Requirement Y: tests fail\n"
		return os.WriteFile(filepath.Join(cfg.RepoDir, "verify-failed.txt"), []byte(content), 0o600)
	}

	err = r.Verify()
	if err == nil {
		t.Fatal("expected error when verification fails")
	}
	if !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("error = %q, want verification failed message", err)
	}
}

func TestVerifyEmptyFunctional(t *testing.T) {
	env := setupTestEnv(t)

	// Write empty functional.md.
	writeFile(t, filepath.Join(env.DesignDir, "functional.md"), "")

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir
	r.Claude = mockClaude

	err = r.Verify()
	if err == nil {
		t.Fatal("expected error when functional.md is empty")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q, want empty message", err)
	}
}

func TestVerifyDocumentContents(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Capture the document.
	var captured string
	r.Claude = func(_ context.Context, cfg ClaudeRunConfig) error {
		captured = cfg.Document
		// Create verify-passed.txt so the flow completes.
		return os.WriteFile(filepath.Join(cfg.RepoDir, "verify-passed.txt"), []byte("PASS"), 0o600)
	}

	if err := r.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// Verify document contains functional.md content.
	if !strings.Contains(captured, "Tests must pass.") {
		t.Error("document missing functional.md content")
	}

	// Verify document contains mission.
	if !strings.Contains(captured, "# Mission") {
		t.Error("document missing Mission section")
	}

	// Verify document contains verification instructions.
	if !strings.Contains(captured, "# Verification Instructions") {
		t.Error("document missing Verification Instructions section")
	}

	// Verify document mentions verify-passed.txt / verify-failed.txt.
	if !strings.Contains(captured, "verify-passed.txt") {
		t.Error("document missing verify-passed.txt reference")
	}
	if !strings.Contains(captured, "verify-failed.txt") {
		t.Error("document missing verify-failed.txt reference")
	}

	// Verify document contains absolute path to functional.md.
	functionalPath := filepath.Join(env.DesignDir, "functional.md")
	if !strings.Contains(captured, functionalPath) {
		t.Errorf("document missing absolute path to functional.md (%s)", functionalPath)
	}

	// Verify document instructs Claude to update functional.md.
	if !strings.Contains(captured, "Functional Specification Updates") {
		t.Error("document missing Functional Specification Updates section")
	}
}

func TestVerifyClaudeFailure(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir
	r.Claude = mockClaudeFailing

	err = r.Verify()
	if err == nil {
		t.Fatal("expected error when Claude fails")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error = %q, want claude error", err)
	}
}

func TestVerifyNoResultFiles(t *testing.T) {
	env := setupTestEnv(t)

	r, err := New(env.Config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.BaseDir = env.BaseDir

	// Mock Claude that doesn't create any result files.
	r.Claude = func(_ context.Context, _ ClaudeRunConfig) error {
		return nil
	}

	err = r.Verify()
	if err == nil {
		t.Fatal("expected error when no result files produced")
	}
	if !strings.Contains(err.Error(), "did not produce") {
		t.Errorf("error = %q, want 'did not produce' message", err)
	}
}
