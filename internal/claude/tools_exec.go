package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ValidatePath resolves a tool path relative to repoDir and rejects directory traversal.
func ValidatePath(repoDir, rawPath string) (string, error) {
	abs := rawPath
	if !filepath.IsAbs(rawPath) {
		abs = filepath.Join(repoDir, rawPath)
	}
	abs = filepath.Clean(abs)

	rel, err := filepath.Rel(repoDir, abs)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %w", rawPath, err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q escapes repository root", rawPath)
	}
	return abs, nil
}

// PrepareMeta builds display metadata for a tool invocation before execution.
func PrepareMeta(repoDir, name string, input json.RawMessage) ToolMeta {
	meta := ToolMeta{Kind: ToolKindFor(name)}

	var params map[string]string
	_ = json.Unmarshal(input, &params)

	switch name {
	case toolReadFile:
		meta.Path = params["path"]
	case toolWriteFile:
		meta.Path = params["path"]
		meta.Content = params["content"]
		if absPath, err := ValidatePath(repoDir, params["path"]); err == nil {
			if old, err := os.ReadFile(absPath); err == nil { //nolint:gosec // path validated
				meta.Diff = computeUnifiedDiff(params["path"], string(old), params["content"])
			} else {
				meta.Diff = computeUnifiedDiff(params["path"], "", params["content"])
			}
		}
	case toolEditFile:
		meta.Path = params["path"]
		if absPath, err := ValidatePath(repoDir, params["path"]); err == nil {
			if old, err := os.ReadFile(absPath); err == nil { //nolint:gosec // path validated
				newContent := strings.Replace(string(old), params["old_text"], params["new_text"], 1)
				meta.Diff = computeUnifiedDiff(params["path"], string(old), newContent)
			}
		}
	case toolBash:
		meta.Command = params["command"]
	case toolListFiles:
		meta.Path = params["path"]
	case toolSearchFiles:
		meta.Path = params["path"]
	}

	return meta
}

// ExecuteTool runs a tool and returns its output.
func ExecuteTool(repoDir, name string, input json.RawMessage) (string, error) {
	var params map[string]string
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid tool input: %w", err)
	}

	switch name {
	case toolReadFile:
		return execReadFile(repoDir, params)
	case toolWriteFile:
		return execWriteFile(repoDir, params)
	case toolEditFile:
		return execEditFile(repoDir, params)
	case toolBash:
		return execBash(repoDir, params)
	case toolListFiles:
		return execListFiles(repoDir, params)
	case toolSearchFiles:
		return execSearchFiles(repoDir, params)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func execReadFile(repoDir string, params map[string]string) (string, error) {
	absPath, err := ValidatePath(repoDir, params["path"])
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(absPath) //nolint:gosec // path validated above
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", params["path"], err)
	}
	return string(data), nil
}

func execWriteFile(repoDir string, params map[string]string) (string, error) {
	absPath, err := ValidatePath(repoDir, params["path"])
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("creating directory: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(params["content"]), 0o600); err != nil {
		return "", fmt.Errorf("writing %s: %w", params["path"], err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(params["content"]), params["path"]), nil
}

func execEditFile(repoDir string, params map[string]string) (string, error) {
	absPath, err := ValidatePath(repoDir, params["path"])
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(absPath) //nolint:gosec // path validated
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", params["path"], err)
	}
	content := string(data)
	if !strings.Contains(content, params["old_text"]) {
		return "", fmt.Errorf("old_text not found in %s", params["path"])
	}
	newContent := strings.Replace(content, params["old_text"], params["new_text"], 1)
	if err := os.WriteFile(absPath, []byte(newContent), 0o600); err != nil {
		return "", fmt.Errorf("writing %s: %w", params["path"], err)
	}
	return "Edited " + params["path"], nil
}

func execBash(repoDir string, params map[string]string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "bash", "-c", params["command"]) //nolint:gosec // user-approved command
	cmd.Dir = repoDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}
	if err != nil {
		return output, fmt.Errorf("command failed: %w\n%s", err, output)
	}
	return output, nil
}

func execListFiles(repoDir string, params map[string]string) (string, error) {
	dirPath := params["path"]
	if dirPath == "" {
		dirPath = "."
	}
	absPath, err := ValidatePath(repoDir, dirPath)
	if err != nil {
		return "", err
	}

	pattern := params["pattern"]
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", fmt.Errorf("listing %s: %w", dirPath, err)
	}

	var lines []string
	for _, e := range entries {
		name := e.Name()
		if pattern != "" {
			matched, _ := filepath.Match(pattern, name)
			if !matched {
				continue
			}
		}
		if e.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}
	return strings.Join(lines, "\n"), nil
}

func execSearchFiles(repoDir string, params map[string]string) (string, error) {
	searchPath := params["path"]
	if searchPath == "" {
		searchPath = "."
	}
	absPath, err := ValidatePath(repoDir, searchPath)
	if err != nil {
		return "", err
	}

	re, err := regexp.Compile(params["pattern"])
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", params["pattern"], err)
	}

	globPattern := params["glob"]
	var results []string

	err = filepath.Walk(absPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // skip unreadable entries
		}
		if info.IsDir() {
			return nil
		}
		if globPattern != "" {
			matched, _ := filepath.Match(globPattern, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		data, readErr := os.ReadFile(path) //nolint:gosec // path from walk
		if readErr != nil {
			return nil //nolint:nilerr // skip unreadable files
		}

		relPath, _ := filepath.Rel(repoDir, path)
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", relPath, i+1, line))
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No matches found.", nil
	}
	// Limit output to avoid huge responses.
	if len(results) > 200 {
		results = results[:200]
		results = append(results, "... (truncated)")
	}
	return strings.Join(results, "\n"), nil
}

// computeUnifiedDiff creates a simple unified diff between old and new content.
func computeUnifiedDiff(path, oldContent, newContent string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var buf strings.Builder
	fmt.Fprintf(&buf, "--- a/%s\n", path)
	fmt.Fprintf(&buf, "+++ b/%s\n", path)

	// Simple line-by-line diff.
	i, j := 0, 0
	for i < len(oldLines) || j < len(newLines) {
		switch {
		case i < len(oldLines) && j < len(newLines) && oldLines[i] == newLines[j]:
			fmt.Fprintf(&buf, " %s\n", oldLines[i])
			i++
			j++
		case i < len(oldLines):
			fmt.Fprintf(&buf, "-%s\n", oldLines[i])
			i++
		default:
			fmt.Fprintf(&buf, "+%s\n", newLines[j])
			j++
		}
	}

	return buf.String()
}
