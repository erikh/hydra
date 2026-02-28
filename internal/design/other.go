package design

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// validateOtherFileName rejects names that could cause path traversal.
func validateOtherFileName(name string) error {
	if strings.Contains(name, "/") {
		return errors.New("file name must not contain '/'")
	}
	if strings.Contains(name, "..") {
		return errors.New("file name must not contain '..'")
	}
	if name == "" {
		return errors.New("file name must not be empty")
	}
	return nil
}

// OtherFiles returns the names of files in the other/ directory.
func (d *Dir) OtherFiles() ([]string, error) {
	otherDir := filepath.Join(d.Path, "other")
	entries, err := os.ReadDir(otherDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading other directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// OtherContent reads and returns the content of a file in other/.
func (d *Dir) OtherContent(name string) (string, error) {
	if err := validateOtherFileName(name); err != nil {
		return "", err
	}

	data, err := os.ReadFile(filepath.Join(d.Path, "other", name)) //nolint:gosec // name validated above
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("other file %q not found", name)
		}
		return "", fmt.Errorf("reading other file %q: %w", name, err)
	}
	return string(data), nil
}

// RemoveOtherFile deletes a file from other/.
func (d *Dir) RemoveOtherFile(name string) error {
	if err := validateOtherFileName(name); err != nil {
		return err
	}

	path := filepath.Join(d.Path, "other", name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("other file %q not found", name)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing other file %q: %w", name, err)
	}
	return nil
}

// AddOtherFile creates a new file in other/ using the editor.
func AddOtherFile(designDir, fileName, editor string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := validateOtherFileName(fileName); err != nil {
		return err
	}

	otherDir := filepath.Join(designDir, "other")
	destPath := filepath.Join(otherDir, fileName)

	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("other file %q already exists", fileName)
	}

	tmpFile, err := os.CreateTemp("", "hydra-other-*.md")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not close temp file: %v\n", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	if err := runEditor(editor, tmpPath, stdin, stdout, stderr); err != nil {
		return err
	}

	content, err := os.ReadFile(tmpPath) //nolint:gosec // path is from our own temp file
	if err != nil {
		return fmt.Errorf("reading temp file: %w", err)
	}

	if len(strings.TrimSpace(string(content))) == 0 {
		return errors.New("empty file, aborting")
	}

	if err := os.MkdirAll(otherDir, 0o750); err != nil {
		return fmt.Errorf("creating other directory: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil { //nolint:gosec // paths are constructed from our own design dir
		if err := os.WriteFile(destPath, content, 0o600); err != nil { //nolint:gosec // paths are constructed from our own design dir
			return fmt.Errorf("writing other file: %w", err)
		}
	}

	return nil
}

// EditOtherFile opens an existing file in other/ in the editor.
func EditOtherFile(designDir, fileName, editor string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := validateOtherFileName(fileName); err != nil {
		return err
	}

	filePath := filepath.Join(designDir, "other", fileName)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("other file %q not found", fileName)
	}

	return runEditor(editor, filePath, stdin, stdout, stderr)
}
