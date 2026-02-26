// Package config manages hydra project configuration stored in the .hydra/ directory.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// HydraDir is the name of the hydra configuration directory.
	HydraDir = ".hydra"
	// ConfigFile is the name of the configuration file within HydraDir.
	ConfigFile = "config.json"
)

// Config holds the hydra project configuration.
type Config struct {
	SourceRepoURL string `json:"source_repo_url"`
	DesignDir     string `json:"design_dir"`
	RepoDir       string `json:"repo_dir"`
}

// HydraPath returns the path to the .hydra directory within base.
func HydraPath(base string) string {
	return filepath.Join(base, HydraDir)
}

// Path returns the path to the config file within base.
func Path(base string) string {
	return filepath.Join(HydraPath(base), ConfigFile)
}

// Init creates a new hydra configuration in the given base directory.
func Init(base, sourceRepoURL, designDir string) (*Config, error) {
	hydraPath := HydraPath(base)
	if err := os.MkdirAll(hydraPath, 0o750); err != nil {
		return nil, fmt.Errorf("creating .hydra directory: %w", err)
	}

	absDesign, err := filepath.Abs(designDir)
	if err != nil {
		return nil, fmt.Errorf("resolving design dir path: %w", err)
	}

	absBase, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("resolving base path: %w", err)
	}

	cfg := &Config{
		SourceRepoURL: sourceRepoURL,
		DesignDir:     absDesign,
		RepoDir:       filepath.Join(absBase, "repo"),
	}

	if err := cfg.Save(base); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Load reads the configuration from the .hydra directory in base.
func Load(base string) (*Config, error) {
	data, err := os.ReadFile(Path(base))
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

// Save writes the configuration to the .hydra directory in base.
func (c *Config) Save(base string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	path := Path(base)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// ErrNoConfig is returned when no configuration file is found.
var ErrNoConfig = errors.New("no hydra configuration found")
