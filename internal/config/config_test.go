package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearEnv blanks every env var the package reads so each test starts from a
// clean slate regardless of the developer's real environment.
func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSITRONICK_CONFIG_DIR", "")
	t.Setenv("POSITRONICK_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", "")
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Dir precedence is a contract for users who relocate their config:
// POSITRONICK_CONFIG_DIR > $XDG_CONFIG_HOME/positronick > ~/.config/positronick.
func TestDirPrecedence(t *testing.T) {
	t.Run("POSITRONICK_CONFIG_DIR wins over XDG and HOME", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("POSITRONICK_CONFIG_DIR", "/explicit/dir")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := Dir()
		if err != nil {
			t.Fatal(err)
		}
		if got != "/explicit/dir" {
			t.Errorf("Dir() = %q, want /explicit/dir", got)
		}
	})

	t.Run("XDG_CONFIG_HOME/positronick wins over HOME", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := Dir()
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join("/xdg", "positronick") {
			t.Errorf("Dir() = %q, want /xdg/positronick", got)
		}
	})

	t.Run("falls back to ~/.config/positronick", func(t *testing.T) {
		clearEnv(t)
		home := t.TempDir()
		t.Setenv("HOME", home)
		got, err := Dir()
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(home, ".config", "positronick") {
			t.Errorf("Dir() = %q, want %q", got, filepath.Join(home, ".config", "positronick"))
		}
	})
}

// The full base-URL precedence matrix: flag > POSITRONICK_BASE_URL >
// config.json base_url > default. Scripts and agents rely on each layer being
// overridable by the one above it.
func TestResolveBaseURLPrecedence(t *testing.T) {
	t.Run("flag wins over env and file", func(t *testing.T) {
		clearEnv(t)
		dir := t.TempDir()
		t.Setenv("POSITRONICK_CONFIG_DIR", dir)
		t.Setenv("POSITRONICK_BASE_URL", "https://env.example.com")
		writeConfig(t, dir, `{"base_url":"https://file.example.com"}`)

		got, err := ResolveBaseURL("https://flag.example.com")
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://flag.example.com" {
			t.Errorf("ResolveBaseURL = %q, want flag value", got)
		}
	})

	t.Run("env wins over file", func(t *testing.T) {
		clearEnv(t)
		dir := t.TempDir()
		t.Setenv("POSITRONICK_CONFIG_DIR", dir)
		t.Setenv("POSITRONICK_BASE_URL", "https://env.example.com")
		writeConfig(t, dir, `{"base_url":"https://file.example.com"}`)

		got, err := ResolveBaseURL("")
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://env.example.com" {
			t.Errorf("ResolveBaseURL = %q, want env value", got)
		}
	})

	t.Run("file wins over default", func(t *testing.T) {
		clearEnv(t)
		dir := t.TempDir()
		t.Setenv("POSITRONICK_CONFIG_DIR", dir)
		writeConfig(t, dir, `{"base_url":"https://file.example.com"}`)

		got, err := ResolveBaseURL("")
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://file.example.com" {
			t.Errorf("ResolveBaseURL = %q, want file value", got)
		}
	})

	t.Run("missing config file falls through to the default", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("POSITRONICK_CONFIG_DIR", t.TempDir())

		got, err := ResolveBaseURL("")
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://positronick.com" {
			t.Errorf("ResolveBaseURL = %q, want default", got)
		}
	})

	t.Run("config file without base_url falls through to the default", func(t *testing.T) {
		clearEnv(t)
		dir := t.TempDir()
		t.Setenv("POSITRONICK_CONFIG_DIR", dir)
		writeConfig(t, dir, `{}`)

		got, err := ResolveBaseURL("")
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://positronick.com" {
			t.Errorf("ResolveBaseURL = %q, want default", got)
		}
	})
}

// Fail-loud posture: a broken config file must surface, not be silently
// ignored — otherwise a typo sends every request to the wrong host.
func TestResolveBaseURLMalformedConfig(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	t.Setenv("POSITRONICK_CONFIG_DIR", dir)
	writeConfig(t, dir, `{not json`)

	_, err := ResolveBaseURL("")
	if err == nil {
		t.Fatal("malformed config.json must produce an error")
	}
	if !strings.Contains(err.Error(), "config.json") {
		t.Errorf("error should name the offending file, got %q", err)
	}
}

func TestResolveBaseURLValidation(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string // substring of the expected error; empty = no error
	}{
		{"https accepted", "https://positronick.com", "https://positronick.com", ""},
		{"http accepted for local dev", "http://localhost:5173", "http://localhost:5173", ""},
		{"trailing slash stripped", "https://positronick.com/", "https://positronick.com", ""},
		{"ftp rejected with actionable message", "ftp://positronick.com", "", "http or https"},
		{"scheme-less rejected", "positronick.com", "", "http or https"},
		{"garbage rejected", "://nope", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			got, err := ResolveBaseURL(tt.in)
			if tt.want != "" {
				if err != nil {
					t.Fatalf("ResolveBaseURL(%q) error: %v", tt.in, err)
				}
				if got != tt.want {
					t.Errorf("ResolveBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("ResolveBaseURL(%q) should error", tt.in)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should mention %q", err, tt.wantErr)
			}
		})
	}
}

// The env and file layers must be validated too — a bad POSITRONICK_BASE_URL
// should fail loudly, not fall through to the default.
func TestResolveBaseURLValidatesEveryLayer(t *testing.T) {
	t.Run("invalid env value errors", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("POSITRONICK_CONFIG_DIR", t.TempDir())
		t.Setenv("POSITRONICK_BASE_URL", "ftp://env.example.com")
		if _, err := ResolveBaseURL(""); err == nil {
			t.Fatal("invalid POSITRONICK_BASE_URL must error, not fall through")
		}
	})

	t.Run("invalid file value errors", func(t *testing.T) {
		clearEnv(t)
		dir := t.TempDir()
		t.Setenv("POSITRONICK_CONFIG_DIR", dir)
		writeConfig(t, dir, `{"base_url":"ftp://file.example.com"}`)
		if _, err := ResolveBaseURL(""); err == nil {
			t.Fatal("invalid config.json base_url must error, not fall through")
		}
	})
}
