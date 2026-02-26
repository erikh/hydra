package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials holds the API authentication details.
type Credentials struct {
	APIKey       string
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

// LoadCredentials resolves API credentials.
// It checks ANTHROPIC_API_KEY first, then falls back to ~/.claude/.credentials.json.
func LoadCredentials() (*Credentials, error) {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return &Credentials{APIKey: key}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.New("no ANTHROPIC_API_KEY set and cannot determine home directory")
	}

	credPath := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(credPath) //nolint:gosec // standard credential file location
	if err != nil {
		return nil, errors.New("no ANTHROPIC_API_KEY set and ~/.claude/.credentials.json not found")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("malformed credentials file: %w", err)
	}

	oauthRaw, ok := raw["claudeAiOauth"]
	if !ok {
		return nil, errors.New("credentials file missing claudeAiOauth key")
	}

	var oauth struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	}
	if err := json.Unmarshal(oauthRaw, &oauth); err != nil {
		return nil, fmt.Errorf("malformed OAuth section in credentials: %w", err)
	}

	if oauth.AccessToken == "" {
		return nil, errors.New("credentials file has empty accessToken")
	}

	return &Credentials{
		AccessToken:  oauth.AccessToken,
		RefreshToken: oauth.RefreshToken,
		ExpiresAt:    oauth.ExpiresAt,
	}, nil
}
