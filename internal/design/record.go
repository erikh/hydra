package design

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Record maps commit SHAs to the task documents that produced them.
type Record struct {
	path string // {designDir}/state/record.json
}

// RecordEntry represents a single SHA -> task name mapping.
type RecordEntry struct {
	SHA      string `json:"sha"`
	TaskName string `json:"task_name"`
}

// NewRecord opens or creates a record at {designDir}/state/record.json.
func NewRecord(designDir string) *Record {
	return &Record{
		path: filepath.Join(designDir, "state", "record.json"),
	}
}

// Add appends a SHA -> task name mapping to the record.
func (r *Record) Add(sha, taskName string) error {
	entries, err := r.Entries()
	if err != nil {
		return err
	}

	entries = append(entries, RecordEntry{SHA: sha, TaskName: taskName})

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling record: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(r.path), 0o750); err != nil {
		return fmt.Errorf("creating record directory: %w", err)
	}

	if err := os.WriteFile(r.path, data, 0o600); err != nil {
		return fmt.Errorf("writing record: %w", err)
	}

	return nil
}

// Entries returns all recorded SHA -> task name entries.
func (r *Record) Entries() ([]RecordEntry, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading record: %w", err)
	}

	var entries []RecordEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing record: %w", err)
	}

	return entries, nil
}
