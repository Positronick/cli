package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// Provider precedence is a security contract: an explicit POSITRONICK_API_KEY
// always wins over the cached bearer, and the cached bearer is only used for
// the base URL it was issued against.
func TestProviderPrecedence(t *testing.T) {
	dir := t.TempDir()
	creds := testCreds() // baseUrl https://positronick.com, token tok-123
	if err := Save(dir, creds); err != nil {
		t.Fatalf("Save: %v", err)
	}

	t.Run("env API key beats the cached bearer", func(t *testing.T) {
		t.Setenv("POSITRONICK_API_KEY", "posi_env_key")
		got, err := NewProvider(dir, "https://positronick.com").Get()
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.APIKey != "posi_env_key" {
			t.Errorf("APIKey = %q, want the env key", got.APIKey)
		}
		if got.Bearer != "" {
			t.Errorf("Bearer = %q, must be empty when the env key wins", got.Bearer)
		}
	})

	t.Run("cached bearer when no env key", func(t *testing.T) {
		t.Setenv("POSITRONICK_API_KEY", "")
		got, err := NewProvider(dir, "https://positronick.com").Get()
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.APIKey != "" || got.Bearer != "tok-123" {
			t.Errorf("creds = %+v, want only the cached bearer", got)
		}
	})

	t.Run("cached bearer for another base URL is never sent", func(t *testing.T) {
		t.Setenv("POSITRONICK_API_KEY", "")
		got, err := NewProvider(dir, "http://localhost:5173").Get()
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.APIKey != "" || got.Bearer != "" {
			t.Errorf("creds = %+v, want anonymous for a different base URL", got)
		}
	})

	t.Run("nothing cached and no env key is anonymous", func(t *testing.T) {
		t.Setenv("POSITRONICK_API_KEY", "")
		got, err := NewProvider(t.TempDir(), "https://positronick.com").Get()
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.APIKey != "" || got.Bearer != "" {
			t.Errorf("creds = %+v, want anonymous", got)
		}
	})
}

// The env key must be read at call time (agents export it after setup) —
// same behavior api.EnvCredentials had before the provider replaced it.
func TestProviderReadsEnvAtCallTime(t *testing.T) {
	p := NewProvider(t.TempDir(), "https://positronick.com")
	t.Setenv("POSITRONICK_API_KEY", "posi_late_key") // set AFTER construction
	got, err := p.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.APIKey != "posi_late_key" {
		t.Errorf("APIKey = %q, want the late-exported key", got.APIKey)
	}
}

// A corrupt credential cache fails loudly instead of silently degrading to
// anonymous requests.
func TestProviderCorruptStore(t *testing.T) {
	t.Setenv("POSITRONICK_API_KEY", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvider(dir, "https://positronick.com").Get(); err == nil {
		t.Fatal("a corrupt credentials.json must surface an error")
	}
}

// TokenCredentials pins a specific bearer token — used where the session
// token must be used even when an env API key is present (key minting).
func TestTokenCredentials(t *testing.T) {
	t.Setenv("POSITRONICK_API_KEY", "posi_env_key")
	got, err := TokenCredentials{Token: "tok-9"}.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Bearer != "tok-9" || got.APIKey != "" {
		t.Errorf("creds = %+v, want only the fixed bearer", got)
	}
}
