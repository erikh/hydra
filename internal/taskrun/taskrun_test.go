package taskrun

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hydra.yml")

	content := "commands:\n  lint: \"echo lint\"\n  test: \"echo test\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cmds, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cmds.Commands["lint"] != "echo lint" {
		t.Errorf("lint = %q, want %q", cmds.Commands["lint"], "echo lint")
	}
	if cmds.Commands["test"] != "echo test" {
		t.Errorf("test = %q, want %q", cmds.Commands["test"], "echo test")
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("/nonexistent/hydra.yml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadEmptyCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hydra.yml")

	if err := os.WriteFile(path, []byte("commands:\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmds, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cmds.Commands) != 0 {
		t.Errorf("expected empty commands, got %d", len(cmds.Commands))
	}
}

func TestLoadTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hydra.yml")

	content := "timeout: \"45m\"\ncommands:\n  test: \"echo test\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cmds, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cmds.Timeout == nil {
		t.Fatal("expected timeout to be set")
	}
	if cmds.Timeout.Duration != 45*time.Minute {
		t.Errorf("timeout = %v, want 45m", cmds.Timeout.Duration)
	}
}

func TestLoadTimeoutNotSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hydra.yml")

	content := "commands:\n  test: \"echo test\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cmds, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cmds.Timeout != nil {
		t.Errorf("expected nil timeout when not set, got %v", cmds.Timeout.Duration)
	}
}

func TestLoadTimeoutInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hydra.yml")

	content := "timeout: \"not-a-duration\"\ncommands:\n  test: \"echo test\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid timeout duration")
	}
}

func TestRunSuccess(t *testing.T) {
	dir := t.TempDir()
	cmds := &Commands{
		Commands: map[string]string{
			"test": "true",
		},
	}

	if err := cmds.Run("test", dir); err != nil {
		t.Fatalf("Run test: %v", err)
	}
}

func TestRunFailure(t *testing.T) {
	dir := t.TempDir()
	cmds := &Commands{
		Commands: map[string]string{
			"test": "false",
		},
	}

	err := cmds.Run("test", dir)
	if err == nil {
		t.Fatal("expected error for failing command")
	}
}

func TestRunUndefined(t *testing.T) {
	dir := t.TempDir()
	cmds := &Commands{
		Commands: map[string]string{},
	}

	// Running an undefined command should succeed (skip silently).
	if err := cmds.Run("nonexistent", dir); err != nil {
		t.Fatalf("Run undefined: %v", err)
	}
}

func TestRunWithArgs(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "output.txt")

	cmds := &Commands{
		Commands: map[string]string{
			"test": "touch " + outFile,
		},
	}

	if err := cmds.Run("test", dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(outFile); err != nil {
		t.Errorf("output file not created: %v", err)
	}
}
