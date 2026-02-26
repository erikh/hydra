package design

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Milestones returns all milestones found in the milestone/ directory.
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
