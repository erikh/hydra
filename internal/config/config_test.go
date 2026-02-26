package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHydraPath(t *testing.T) {
	got := HydraPath("/tmp/project")
	want := filepath.Join("/tmp/project", HydraDir)
	if got != want {
		t.Errorf("HydraPath = %q, want %q", got, want)
	}
}

func TestPath(t *testing.T) {
	got := Path("/tmp/project")
	want := filepath.Join("/tmp/project", HydraDir, ConfigFile)
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

func TestInitCreatesDirectoryAndConfig(t *testing.T) {
	base := t.TempDir()
	designDir := t.TempDir()

	cfg, err := Init(base, "https://github.com/test/repo.git", designDir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if cfg.SourceRepoURL != "https://github.com/test/repo.git" {
		t.Errorf("SourceRepoURL = %q, want test URL", cfg.SourceRepoURL)
	}

	absDesign, _ := filepath.Abs(designDir)
	if cfg.DesignDir != absDesign {
		t.Errorf("DesignDir = %q, want %q", cfg.DesignDir, absDesign)
	}

	expectedRepoDir := filepath.Join(base, HydraDir, "repo")
	if cfg.RepoDir != expectedRepoDir {
		t.Errorf("RepoDir = %q, want %q", cfg.RepoDir, expectedRepoDir)
	}

	// Verify .hydra directory exists
	info, err := os.Stat(HydraPath(base))
	if err != nil {
		t.Fatalf(".hydra dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".hydra is not a directory")
	}

	// Verify config file exists and is valid JSON
	data, err := os.ReadFile(Path(base))
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("config file is not valid JSON: %v", err)
	}

	if loaded.SourceRepoURL != cfg.SourceRepoURL {
		t.Errorf("persisted SourceRepoURL = %q, want %q", loaded.SourceRepoURL, cfg.SourceRepoURL)
	}
}

func TestLoadRoundTrip(t *testing.T) {
	base := t.TempDir()
	designDir := t.TempDir()

	original, err := Init(base, "https://github.com/test/repo.git", designDir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	loaded, err := Load(base)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.SourceRepoURL != original.SourceRepoURL {
		t.Errorf("SourceRepoURL mismatch: %q vs %q", loaded.SourceRepoURL, original.SourceRepoURL)
	}
	if loaded.DesignDir != original.DesignDir {
		t.Errorf("DesignDir mismatch: %q vs %q", loaded.DesignDir, original.DesignDir)
	}
	if loaded.RepoDir != original.RepoDir {
		t.Errorf("RepoDir mismatch: %q vs %q", loaded.RepoDir, original.RepoDir)
	}
}

func TestLoadMissingConfig(t *testing.T) {
	base := t.TempDir()
	_, err := Load(base)
	if err == nil {
		t.Fatal("Load should fail when config doesn't exist")
	}
}

func TestSaveOverwrite(t *testing.T) {
	base := t.TempDir()
	designDir := t.TempDir()

	cfg, err := Init(base, "https://github.com/test/repo.git", designDir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	cfg.SourceRepoURL = "https://github.com/test/other.git"
	if err := cfg.Save(base); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(base)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.SourceRepoURL != "https://github.com/test/other.git" {
		t.Errorf("SourceRepoURL = %q, want updated URL", loaded.SourceRepoURL)
	}
}
