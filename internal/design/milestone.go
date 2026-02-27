package design

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Milestone represents a milestone target read from milestone/{date}.md.
type Milestone struct {
	Date     string // from filename
	FilePath string
}

// MilestoneHistory represents a historical milestone from milestone/history/{date}-{score}.md.
type MilestoneHistory struct {
	Date     string
	Score    string // A-F
	FilePath string
}

// Promise represents a single ## heading in a milestone file.
type Promise struct {
	Heading string // text of ## heading
	Body    string // content between this heading and next ## or EOF
	Slug    string // filename-safe slug derived from Heading
}

// VerifyResult holds the outcome of verifying a milestone's promises against tasks.
type VerifyResult struct {
	Date       string
	Promises   []Promise
	Missing    []string // promise slugs with no task in any state
	Incomplete []string // slugs with task not in completed state
	AllKept    bool
}

// RepairResult holds the outcome of repairing a milestone's task group.
type RepairResult struct {
	Created []string
	Skipped []string
}

// MilestoneTemplate is the starter content for a new milestone file.
const MilestoneTemplate = `<!-- Milestone: promises to keep by the target date.

Each promise is a level-2 heading (##). Write details under each heading.
Hydra tracks these headings as commitments. HTML comments like this one
are ignored and won't appear as promises.

Example:

## Ship user authentication
Implement login, registration, and password reset flows.
All endpoints must have integration tests.

## Reach 80% code coverage
Add unit tests for all packages in internal/.
-->

##
`

var nonAlphaNumRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts a string into a filename-safe slug.
func Slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlphaNumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// NormalizeDate parses a date in various formats and returns YYYY-MM-DD.
func NormalizeDate(input string) (string, error) {
	formats := []string{
		"2006-01-02",
		"2006/01/02",
		"01-02-2006",
		"01/02/2006",
	}
	for _, f := range formats {
		t, err := time.Parse(f, input)
		if err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("unrecognized date format: %q (expected YYYY-MM-DD, YYYY/MM/DD, MM-DD-YYYY, or MM/DD/YYYY)", input)
}

// ParsePromises scans markdown content and returns all ## headings as promises.
// HTML comments are stripped before parsing.
func ParsePromises(content string) []Promise {
	// Strip HTML comments.
	cleaned := stripHTMLComments(content)

	lines := strings.Split(cleaned, "\n")
	var promises []Promise
	var current *Promise

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if current != nil && current.Heading != "" {
				current.Body = strings.TrimSpace(current.Body)
				promises = append(promises, *current)
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if heading == "" {
				current = nil
				continue
			}
			current = &Promise{
				Heading: heading,
				Slug:    Slugify(heading),
			}
		} else if current != nil {
			current.Body += line + "\n"
		}
	}

	if current != nil && current.Heading != "" {
		current.Body = strings.TrimSpace(current.Body)
		promises = append(promises, *current)
	}

	return promises
}

// stripHTMLComments removes <!-- ... --> blocks from content.
func stripHTMLComments(s string) string {
	var result strings.Builder
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			result.WriteString(s)
			break
		}
		result.WriteString(s[:start])
		end := strings.Index(s[start:], "-->")
		if end < 0 {
			// Unterminated comment, drop the rest.
			break
		}
		s = s[start+end+3:]
	}
	return result.String()
}

// MilestoneTaskGroup returns the task group name for a milestone date.
func MilestoneTaskGroup(date string) string {
	return "milestone-" + date
}

// Content reads and returns the milestone's markdown content.
func (m *Milestone) Content() (string, error) {
	data, err := os.ReadFile(m.FilePath)
	if err != nil {
		return "", fmt.Errorf("reading milestone %s: %w", m.Date, err)
	}
	return string(data), nil
}

// Milestones returns all undelivered milestones found in the milestone/ directory.
func (d *Dir) Milestones() ([]Milestone, error) {
	dir := filepath.Join(d.Path, "milestone")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading milestone dir: %w", err)
	}

	var milestones []Milestone
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		date := strings.TrimSuffix(entry.Name(), ".md")
		milestones = append(milestones, Milestone{
			Date:     date,
			FilePath: filepath.Join(dir, entry.Name()),
		})
	}

	return milestones, nil
}

// MilestoneHistory returns all historical milestones from milestone/history/.
func (d *Dir) MilestoneHistory() ([]MilestoneHistory, error) {
	dir := filepath.Join(d.Path, "milestone", "history")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading milestone history dir: %w", err)
	}

	var history []MilestoneHistory
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		// Parse {date}-{score} where score is the last character after the last hyphen.
		lastHyphen := strings.LastIndex(base, "-")
		if lastHyphen < 0 || lastHyphen >= len(base)-1 {
			continue // skip malformed filenames
		}
		date := base[:lastHyphen]
		score := base[lastHyphen+1:]

		history = append(history, MilestoneHistory{
			Date:     date,
			Score:    score,
			FilePath: filepath.Join(dir, entry.Name()),
		})
	}

	return history, nil
}

// FindMilestone looks up an undelivered milestone by date.
func (d *Dir) FindMilestone(date string) (*Milestone, error) {
	milestones, err := d.Milestones()
	if err != nil {
		return nil, err
	}
	for _, m := range milestones {
		if m.Date == date {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("milestone %q not found", date)
}

// DeliveredMilestones returns milestones from milestone/delivered/.
func (d *Dir) DeliveredMilestones() ([]Milestone, error) {
	dir := filepath.Join(d.Path, "milestone", "delivered")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading delivered milestones: %w", err)
	}

	var milestones []Milestone
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		date := strings.TrimSuffix(entry.Name(), ".md")
		milestones = append(milestones, Milestone{
			Date:     date,
			FilePath: filepath.Join(dir, entry.Name()),
		})
	}

	return milestones, nil
}

// DeliverMilestone moves a milestone file to milestone/delivered/.
func (d *Dir) DeliverMilestone(m *Milestone) error {
	ackDir := filepath.Join(d.Path, "milestone", "delivered")
	if err := os.MkdirAll(ackDir, 0o750); err != nil {
		return fmt.Errorf("creating delivered dir: %w", err)
	}

	destPath := filepath.Join(ackDir, filepath.Base(m.FilePath))
	if err := os.Rename(m.FilePath, destPath); err != nil {
		return fmt.Errorf("moving milestone to delivered: %w", err)
	}

	m.FilePath = destPath
	return nil
}

// CreateMilestone creates a new milestone file at milestone/{date}.md.
func (d *Dir) CreateMilestone(date, content string) (*Milestone, error) {
	dir := filepath.Join(d.Path, "milestone")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating milestone dir: %w", err)
	}

	filePath := filepath.Join(dir, date+".md")
	if _, err := os.Stat(filePath); err == nil {
		return nil, fmt.Errorf("milestone %q already exists", date)
	}

	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("writing milestone: %w", err)
	}

	return &Milestone{Date: date, FilePath: filePath}, nil
}

// VerifyMilestone checks whether all promises in a milestone have completed tasks.
// It looks for tasks in the milestone's task group (pending state) and also checks
// state directories (review, merge, completed, abandoned) by task name, since
// tasks lose their group when moved to a state directory.
func (d *Dir) VerifyMilestone(m *Milestone) (*VerifyResult, error) {
	content, err := m.Content()
	if err != nil {
		return nil, err
	}

	promises := ParsePromises(content)
	group := MilestoneTaskGroup(m.Date)

	// Get all tasks across all states.
	allTasks, err := d.AllTasks()
	if err != nil {
		return nil, err
	}

	// Build a map of slug -> best task state.
	// Pending tasks with matching group are authoritative.
	// State-dir tasks (no group) are matched by name against promise slugs.
	type taskInfo struct {
		state TaskState
	}
	tasksBySlug := make(map[string]taskInfo)

	// Collect promise slugs for matching.
	slugSet := make(map[string]bool)
	for _, p := range promises {
		slugSet[p.Slug] = true
	}

	for _, t := range allTasks {
		if t.Group == group {
			// Task is still in the group (pending state).
			tasksBySlug[t.Name] = taskInfo{state: t.State}
		} else if t.Group == "" && slugSet[t.Name] {
			// Task was moved to a state directory (lost its group).
			// Only record if we haven't already found it in the group.
			if _, exists := tasksBySlug[t.Name]; !exists {
				tasksBySlug[t.Name] = taskInfo{state: t.State}
			}
		}
	}

	result := &VerifyResult{
		Date:     m.Date,
		Promises: promises,
	}

	for _, p := range promises {
		info, exists := tasksBySlug[p.Slug]
		if !exists {
			result.Missing = append(result.Missing, p.Slug)
		} else if info.state != StateCompleted {
			result.Incomplete = append(result.Incomplete, p.Slug)
		}
	}

	result.AllKept = len(result.Missing) == 0 && len(result.Incomplete) == 0
	return result, nil
}

// RepairMilestone creates missing task files for promises that lack them.
func (d *Dir) RepairMilestone(m *Milestone) (*RepairResult, error) {
	content, err := m.Content()
	if err != nil {
		return nil, err
	}

	promises := ParsePromises(content)
	group := MilestoneTaskGroup(m.Date)
	groupDir := filepath.Join(d.Path, "tasks", group)

	// Get existing tasks in this group (any state).
	allTasks, err := d.AllTasks()
	if err != nil {
		return nil, err
	}

	existing := make(map[string]bool)
	for _, t := range allTasks {
		if t.Group == group {
			existing[t.Name] = true
		}
	}

	result := &RepairResult{}

	needsDir := false
	for _, p := range promises {
		if existing[p.Slug] {
			result.Skipped = append(result.Skipped, p.Slug)
			continue
		}
		needsDir = true
	}

	if needsDir {
		if err := os.MkdirAll(groupDir, 0o750); err != nil {
			return nil, fmt.Errorf("creating task group dir: %w", err)
		}

		// Create group.md if it doesn't exist.
		groupFile := filepath.Join(groupDir, "group.md")
		if _, err := os.Stat(groupFile); os.IsNotExist(err) {
			groupContent := fmt.Sprintf("Milestone %s tasks.\n", m.Date)
			if err := os.WriteFile(groupFile, []byte(groupContent), 0o600); err != nil {
				return nil, fmt.Errorf("creating group.md: %w", err)
			}
		}
	}

	for _, p := range promises {
		if existing[p.Slug] {
			continue
		}

		taskContent := "## " + p.Heading + "\n"
		if p.Body != "" {
			taskContent += "\n" + p.Body + "\n"
		}

		taskPath := filepath.Join(groupDir, p.Slug+".md")
		if err := os.WriteFile(taskPath, []byte(taskContent), 0o600); err != nil {
			return nil, fmt.Errorf("creating task %s: %w", p.Slug, err)
		}
		result.Created = append(result.Created, p.Slug)
	}

	return result, nil
}
