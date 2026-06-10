package output

import "testing"

// clearDetectionEnv neutralises every env var DetectMode reads so tests are
// hermetic even when the suite itself runs under CI or a coding agent.
func clearDetectionEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{"CI", "GITHUB_ACTIONS", "CLAUDECODE", "GEMINI_CLI", "CURSOR_EDITOR", "NO_COLOR"} {
		t.Setenv(v, "")
	}
}

// fakeTTY overrides the terminal probe so the TTY-dependent branches
// (color, interactivity) are testable without a real terminal.
func fakeTTY(t *testing.T, tty bool) {
	t.Helper()
	orig := isTerminal
	isTerminal = func(int) bool { return tty }
	t.Cleanup(func() { isTerminal = orig })
}

// Non-interactive auto-detection is a promise the README makes to agents:
// under CI or a coding agent the CLI must never colorise or prompt. These
// tests pin each detection signal and the precedence between them.
func TestDetectMode(t *testing.T) {
	tests := []struct {
		name            string
		env             map[string]string
		tty             bool
		jsonFlag        bool
		noColorFlag     bool
		wantCI          bool
		wantAgent       string
		wantColor       bool
		wantJSON        bool
		wantInteractive bool
	}{
		{
			name:            "interactive terminal gets color and prompts",
			tty:             true,
			wantColor:       true,
			wantInteractive: true,
		},
		{
			name:            "no tty disables color and prompts",
			tty:             false,
			wantColor:       false,
			wantInteractive: false,
		},
		{
			name:            "CI env disables color and prompts",
			env:             map[string]string{"CI": "true"},
			tty:             true,
			wantCI:          true,
			wantColor:       false,
			wantInteractive: false,
		},
		{
			name:            "GITHUB_ACTIONS counts as CI",
			env:             map[string]string{"GITHUB_ACTIONS": "true"},
			tty:             true,
			wantCI:          true,
			wantColor:       false,
			wantInteractive: false,
		},
		{
			name:            "CLAUDECODE detected as claude-code agent",
			env:             map[string]string{"CLAUDECODE": "1"},
			tty:             true,
			wantAgent:       "claude-code",
			wantColor:       false,
			wantInteractive: false,
		},
		{
			name:            "GEMINI_CLI detected as gemini-cli agent",
			env:             map[string]string{"GEMINI_CLI": "1"},
			tty:             true,
			wantAgent:       "gemini-cli",
			wantColor:       false,
			wantInteractive: false,
		},
		{
			name:            "CURSOR_EDITOR detected as cursor agent",
			env:             map[string]string{"CURSOR_EDITOR": "1"},
			tty:             true,
			wantAgent:       "cursor",
			wantColor:       false,
			wantInteractive: false,
		},
		{
			name:            "claude-code wins when several agent vars are set",
			env:             map[string]string{"CLAUDECODE": "1", "GEMINI_CLI": "1", "CURSOR_EDITOR": "1"},
			tty:             true,
			wantAgent:       "claude-code",
			wantColor:       false,
			wantInteractive: false,
		},
		{
			name:            "NO_COLOR env disables color but not prompts",
			env:             map[string]string{"NO_COLOR": "1"},
			tty:             true,
			wantColor:       false,
			wantInteractive: true,
		},
		{
			name:            "--no-color flag disables color but not prompts",
			tty:             true,
			noColorFlag:     true,
			wantColor:       false,
			wantInteractive: true,
		},
		{
			name:            "--json flag carries through",
			tty:             true,
			jsonFlag:        true,
			wantColor:       true,
			wantJSON:        true,
			wantInteractive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearDetectionEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			fakeTTY(t, tt.tty)

			m := DetectMode(tt.jsonFlag, tt.noColorFlag)

			if m.TTY != tt.tty {
				t.Errorf("TTY = %v, want %v", m.TTY, tt.tty)
			}
			if m.CI != tt.wantCI {
				t.Errorf("CI = %v, want %v", m.CI, tt.wantCI)
			}
			if m.Agent != tt.wantAgent {
				t.Errorf("Agent = %q, want %q", m.Agent, tt.wantAgent)
			}
			if m.Color != tt.wantColor {
				t.Errorf("Color = %v, want %v", m.Color, tt.wantColor)
			}
			if m.JSON != tt.wantJSON {
				t.Errorf("JSON = %v, want %v", m.JSON, tt.wantJSON)
			}
			if m.Interactive() != tt.wantInteractive {
				t.Errorf("Interactive() = %v, want %v", m.Interactive(), tt.wantInteractive)
			}
		})
	}
}
