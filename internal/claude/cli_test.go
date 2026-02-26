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
			name: "prompt only",
			cfg: CLIConfig{
				Prompt: "hello world",
			},
			want: []string{"-p", "hello world"},
		},
		{
			name: "with model",
			cfg: CLIConfig{
				Prompt: "do something",
				Model:  "claude-opus-4-6",
			},
			want: []string{"-p", "do something", "--model", "claude-opus-4-6"},
		},
		{
			name: "with auto accept",
			cfg: CLIConfig{
				Prompt:     "fix bug",
				AutoAccept: true,
			},
			want: []string{"-p", "fix bug", "--dangerously-skip-permissions"},
		},
		{
			name: "all options",
			cfg: CLIConfig{
				Prompt:     "implement feature",
				Model:      "claude-sonnet-4-6",
				AutoAccept: true,
			},
			want: []string{"-p", "implement feature", "--model", "claude-sonnet-4-6", "--dangerously-skip-permissions"},
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
