package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCredentialsFromEnv(t *testing.T) {
	// No credentials file — env var should be used as fallback.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.APIKey != "sk-test-key" {
		t.Errorf("APIKey = %q, want sk-test-key", creds.APIKey)
	}
}

func TestLoadCredentialsFromJSON(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o750); err != nil {
		t.Fatal(err)
	}

	creds := `{"claudeAiOauth":{"accessToken":"oauth-token-123","refreshToken":"refresh-456","expiresAt":1700000000}}` //nolint:gosec // test data
	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if result.AccessToken != "oauth-token-123" {
		t.Errorf("AccessToken = %q, want oauth-token-123", result.AccessToken)
	}
	if result.RefreshToken != "refresh-456" {
		t.Errorf("RefreshToken = %q, want refresh-456", result.RefreshToken)
	}
	if result.ExpiresAt != 1700000000 {
		t.Errorf("ExpiresAt = %d, want 1700000000", result.ExpiresAt)
	}
}

func TestLoadCredentialsFileTakesPrecedence(t *testing.T) {
	// When both credentials file and env var exist, the file should win.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-be-used")

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o750); err != nil {
		t.Fatal(err)
	}

	creds := `{"claudeAiOauth":{"accessToken":"oauth-from-file","refreshToken":"refresh","expiresAt":1700000000}}` //nolint:gosec // test data
	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if result.AccessToken != "oauth-from-file" {
		t.Errorf("AccessToken = %q, want oauth-from-file (file should take precedence over env)", result.AccessToken)
	}
	if result.APIKey != "" {
		t.Errorf("APIKey = %q, want empty (file credentials should be used, not env)", result.APIKey)
	}
}

func TestLoadCredentialsMissingBoth(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := LoadCredentials()
	if err == nil {
		t.Fatal("expected error when both env and file are missing")
	}
}

func TestLoadCredentialsMalformedJSON(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadCredentials()
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}
