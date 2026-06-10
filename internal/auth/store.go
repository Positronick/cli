package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// credentialsFile is the cache file name inside the config dir.
const credentialsFile = "credentials.json"

// warnWriter receives the loose-permissions warning; a seam so tests can
// capture it.
var warnWriter io.Writer = os.Stderr

// StoredUser is the cached identity from /api/me. IsAdmin is server-computed
// and cached so a later PR can reveal admin commands without a network call.
type StoredUser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"isAdmin"`
}

// Credentials is the on-disk shape of <config dir>/credentials.json. BaseURL
// keys the cache: a token is only ever presented to the host that issued it.
// ExpiresAt is informational — the session lifetime slides server-side, so
// the server stays the authority and a 401 (not the clock) triggers re-login.
type Credentials struct {
	Version     int        `json:"version"`
	BaseURL     string     `json:"baseUrl"`
	AccessToken string     `json:"accessToken"`
	TokenType   string     `json:"tokenType"`
	ExpiresAt   string     `json:"expiresAt"`
	User        StoredUser `json:"user"`
	CreatedAt   string     `json:"createdAt"`
}

// credentialsPath returns the cache file path for a config dir.
func credentialsPath(dir string) string {
	return filepath.Join(dir, credentialsFile)
}

// Save writes the credential cache atomically (temp file + rename) with
// owner-only permissions, creating the config dir (0700) if needed.
func Save(dir string, creds *Credentials) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	payload, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding credentials: %w", err)
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("writing credentials: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing credentials: %w", err)
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing credentials: %w", err)
	}
	if err := os.Rename(tmp.Name(), credentialsPath(dir)); err != nil {
		return fmt.Errorf("writing credentials: %w", err)
	}
	return nil
}

// Load reads the credential cache. It returns (nil, nil) when no credentials
// exist or when the cached baseUrl differs from baseURL — a token must never
// be sent to a host other than the one that issued it. A file readable by
// group/other is repaired to 0600 with a one-line warning: it holds a live
// session token. A malformed file is an error (fix: positronick logout).
func Load(dir, baseURL string) (*Credentials, error) {
	path := credentialsPath(dir)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("fixing permissions on %s: %w", path, err)
		}
		fmt.Fprintf(warnWriter, "warning: %s was group/other-readable; permissions tightened to 0600\n", path)
	}

	var creds Credentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, fmt.Errorf("parsing %s: %w (run `positronick logout` to reset)", path, err)
	}
	if creds.BaseURL != baseURL {
		return nil, nil
	}
	return &creds, nil
}

// Delete removes the credential cache (logout). Nothing cached is fine.
func Delete(dir string) error {
	err := os.Remove(credentialsPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
