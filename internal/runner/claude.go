package runner

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/erikh/hydra/internal/claude"
	"github.com/erikh/hydra/internal/tui"
)

func invokeClaude(ctx context.Context, cfg ClaudeRunConfig) error {
	creds, err := claude.LoadCredentials()
	if err != nil {
		return fmt.Errorf("loading credentials: %w", err)
	}

	model := cfg.Model
	if model == "" {
		model = claude.DefaultModel
	}

	client, err := claude.NewClient(creds, claude.ClientConfig{
		Model:   model,
		RepoDir: cfg.RepoDir,
	})
	if err != nil {
		return fmt.Errorf("creating API client: %w", err)
	}

	session := claude.NewSession(client)
	session.Start(ctx, cfg.Document)

	m := tui.New(session, model, cfg.AutoAccept)
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if fm, ok := finalModel.(tui.Model); ok {
		if tuiErr := fm.Err(); tuiErr != nil {
			return fmt.Errorf("session error: %w", tuiErr)
		}
	}

	return nil
}
