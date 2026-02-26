package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	HydraDir   = ".hydra"
	ConfigFile = "config.json"
)

type Config struct {
	SourceRepoURL string `json:"source_repo_url"`
	DesignDir     string `json:"design_dir"`
	RepoDir       string `json:"repo_dir"`
}

func HydraPath(base string) string {
	return filepath.Join(base, HydraDir)
}

func ConfigPath(base string) string {
	return filepath.Join(HydraPath(base), ConfigFile)
}

func Init(base, sourceRepoURL, designDir string) (*Config, error) {
	hydraPath := HydraPath(base)
	if err := os.MkdirAll(hydraPath, 0o755); err != nil {
		return nil, fmt.Errorf("creating .hydra directory: %w", err)
	}

	absDesign, err := filepath.Abs(designDir)
	if err != nil {
		return nil, fmt.Errorf("resolving design dir path: %w", err)
	}

	cfg := &Config{
		SourceRepoURL: sourceRepoURL,
		DesignDir:     absDesign,
		RepoDir:       filepath.Join(hydraPath, "repo"),
	}

	if err := cfg.Save(base); err != nil {
		return nil, err
	}

	return cfg, nil
}

func Load(base string) (*Config, error) {
	data, err := os.ReadFile(ConfigPath(base))
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Save(base string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(ConfigPath(base), data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}
