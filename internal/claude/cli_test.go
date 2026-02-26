package claude

import (
	"testing"
)

func TestFindCLI(t *testing.T) {
	// Clear PATH so claude won't be found.
	t.Setenv("PATH", "")

	if got := FindCLI(); got != "" {
		t.Errorf("FindCLI() = %q, want empty string when binary not in PATH", got)
	}
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		cfg  CLIConfig
		want []string
	}{
		{
			name: "no flags (prompt only)",
			cfg: CLIConfig{
				Prompt: "hello world",
			},
			want: []string{"hello world"},
		},
		{
			name: "default: auto-accept and plan",
			cfg: CLIConfig{
				Prompt:     "hello world",
				AutoAccept: true,
				PlanMode:   true,
			},
			want: []string{"--dangerously-skip-permissions", "--permission-mode", "plan", "hello world"},
		},
		{
			name: "plan only (no-auto-accept)",
			cfg: CLIConfig{
				Prompt:   "do something",
				PlanMode: true,
			},
			want: []string{"--permission-mode", "plan", "do something"},
		},
		{
			name: "auto-accept only (no-plan)",
			cfg: CLIConfig{
				Prompt:     "fix bug",
				AutoAccept: true,
			},
			want: []string{"--permission-mode", "bypassPermissions", "fix bug"},
		},
		{
			name: "with model and defaults",
			cfg: CLIConfig{
				Prompt:     "do something",
				Model:      "claude-opus-4-6",
				AutoAccept: true,
				PlanMode:   true,
			},
			want: []string{"--model", "claude-opus-4-6", "--dangerously-skip-permissions", "--permission-mode", "plan", "do something"},
		},
		{
			name: "with model, plan only",
			cfg: CLIConfig{
				Prompt:   "do something",
				Model:    "claude-opus-4-6",
				PlanMode: true,
			},
			want: []string{"--model", "claude-opus-4-6", "--permission-mode", "plan", "do something"},
		},
		{
			name: "with model, auto-accept only",
			cfg: CLIConfig{
				Prompt:     "do something",
				Model:      "claude-sonnet-4-6",
				AutoAccept: true,
			},
			want: []string{"--model", "claude-sonnet-4-6", "--permission-mode", "bypassPermissions", "do something"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildArgs(tt.cfg)
			if len(got) != len(tt.want) {
				t.Fatalf("BuildArgs() returned %d args, want %d\n  got:  %v\n  want: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("BuildArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
